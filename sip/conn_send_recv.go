package sip

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"braces.dev/errtrace"
)

// sendLoop is sending the requests it gets from the request queue. Before it
// sends the request, it is waiting that a new transaction is allowed, which
// might not be the case when the number of concurrent transactions is limited.
// When sending fails, it cancels all open requests, stops the receiveLoop and
// returns. If sending is ok, the sendLoop signals to the receiveLoop that a new
// transaction has been started.
func (c *conn) sendLoop(ctx context.Context, cancel context.CancelCauseFunc) {
	err := func() error {
		for {
			select {
			case <-ctx.Done():
				return errtrace.Wrap(context.Cause(ctx))
			// Get a new request
			case req, ok := <-c.dequeueRequest():
				if !ok {
					return errtrace.Wrap(ErrorClosed)
				}
				if err := wait(c.transactionAllowed); err != nil {
					return errtrace.Wrap(err)
				}
				if err := c.send(req); err != nil {
					cancel(errtrace.Wrap(err))
					return errtrace.Wrap(err)
				}
				// Inform receiveLoop there's a new transaction initiated
				if err := signal(c.transactionStarted); err != nil {
					return errtrace.Wrap(err)
				}
			}
		}
	}()
	logger.Printf("breaking sendLoop: %v", err)
}

// receiveLoop receives responses from the server and dispatches them to . Before
// it starts listening on the net.Conn for responses, it waits for the sendLoop
// signaling that a transaction has been started. After the response has been
// read and dispatched to the ReadXXX methods, it signals the sendLoop that a new
// (concurrent) transaction is now allowed. If receiving fails, it cancels all
// open requests, stops the sendLoop and returns.
func (c *conn) receiveLoop(ctx context.Context, cancel context.CancelCauseFunc) {
	err := func() error {
		for {
			select {
			case <-ctx.Done():
				return errtrace.Wrap(context.Cause(ctx))
			default:
				// Wait for an initiated transaction
				if err := wait(c.transactionStarted); err != nil {
					return errtrace.Wrap(err)
				}
				if err := c.receiveAndDispatch(); err != nil {
					cancel(errtrace.Wrap(err))
					return errtrace.Wrap(err)
				}
				// decrease the number of currently running req/resp pairs
				if err := signal(c.transactionAllowed); err != nil {
					return errtrace.Wrap(err)
				}
			}
		}
	}()
	logger.Printf("breaking receiveLoop: %v", err)
}

// dequeueRequest fetches a request from the request queue.
func (c *conn) dequeueRequest() <-chan request {
	c.mxState.Lock()
	defer c.mxState.Unlock()

	if c.reqCh != nil {
		return c.reqCh
	}
	// Connection closed, return closed channel
	ch := make(chan request)
	close(ch)
	return ch
}

// enqueueRequest puts a request into the request queue.
func (c *conn) enqueueRequest(req request) error {
	ch := func() chan<- request {
		c.mxState.RLock()
		defer c.mxState.RUnlock()

		return c.reqCh
	}()
	// Is the connection closed?
	if ch == nil {
		return errtrace.Wrap(ErrorClosed)
	}
	// Send request job into the queue of the sendLoop
	ch <- req
	return nil
}

// send writes the contents of the request to the net.Conn.
func (c *conn) send(req request) error {
	// The write function of the request is build in sendAndWaitForResponse,
	// where also the transactionID is set.
	transactionID, err := req.write(c.Conn)
	if err != nil {
		return errtrace.Wrap(err)
	}
	func() {
		c.mxRC.Lock()
		defer c.mxRC.Unlock()
		// Store the response channel of the request under the transactionID
		// The receiveAndDispatch will use it when it reads a Header with this
		// transactionID to return the function to read the rest of the PDU
		// to sendAndWaitForResponse
		c.respChans[transactionID] = req.ch
	}()
	return nil
}

// receiveAndDispatch reads from the net.Conn and dispatches
// the responses according to the received transactionIDs.
// receiveAndDispatch lives in the receiveLoop.
func (c *conn) receiveAndDispatch() error {
	c.mxRecv.Lock()
	defer c.mxRecv.Unlock()

	// The respFunc is executed on the receiving goroutine
	var respFunc func(PDU) error
	// prepare waiting for the respFunc to end
	respFuncExecuted := make(chan struct{})

	h := &Header{}

readUntilValidResponse:
	for {
		err := h.Read(c.timeoutReader)
		if err != nil {
			return errtrace.Wrap(err)
		}
		// The header was read, this is the first point in time we can be sure the server has sent something
		c.setLastReceived()
		// Read the header and decide what to do next
		switch h.MessageType {
		case 0:
			return nil // TODO When does this happen?
		case BusyResponseMsgType:
			// Busy PDU is empty, do nothing and wait for the real response
			continue
		case ExceptionMsgType:
			respFunc, err = c.newExceptionResponse(respFuncExecuted)
			if err != nil {
				return errtrace.Wrap(err)
			}
			break readUntilValidResponse
		default:
			respFunc = func(pdu PDU) error {
				defer close(respFuncExecuted)

				if h.MessageType != pdu.MessageType() {
					return errtrace.Wrap(fmt.Errorf(
						"%w. Type %d, Expected: %d, TransactionId: %d",
						ErrorInvalidResponseMessageType,
						h.MessageType, pdu.MessageType(), h.TransactionID))
				}
				// log.Printf("receiving %v, id: %v", pdu.MessageType(), h.TransactionID)
				err = pdu.Read(c.timeoutReader)
				// log.Printf("received %v, id: %v, err: %v", pdu.MessageType(), h.TransactionID, err)
				return errtrace.Wrap(err)
			}
			break readUntilValidResponse
		}
	}
	// Get the response channel of the request for this transactionID and send the respFunc to it
	c.checkoutResponseChan(h.TransactionID) <- respFunc
	// Important: Wait for the current respFunc to read the rest of the message (the PDU) from the net.Conn
	<-respFuncExecuted
	return nil
}

func (c *conn) newExceptionResponse(respFuncExecuted chan struct{}) (func(PDU) error, error) {
	ex := Exception{}
	if err := ex.Read(c.timeoutReader); err != nil {
		return nil, errtrace.Wrap(err)
	}
	return func(PDU) error {
		defer close(respFuncExecuted)

		return errtrace.Wrap(ex)
	}, nil
}

func (c *conn) setLastReceived() {
	c.mxState.Lock()
	defer c.mxState.Unlock()

	c.lastReceived = time.Now()
}

func (c *conn) checkoutResponseChan(tID uint32) chan func(PDU) error {
	c.mxRC.Lock()
	defer c.mxRC.Unlock()

	ch := c.respChans[tID]
	delete(c.respChans, tID)
	return ch
}

func (c *conn) writeHeader(conn io.Writer, pdu PDU) (transactionID uint32, err error) {
	h := Header{
		TransactionID: atomic.AddUint32(&c.transactionID, 1),
		MessageType:   pdu.MessageType(),
	}
	return h.TransactionID, errtrace.Wrap(h.Write(conn))
}

func sendRequestWaitForResponseAndRead[Response PDU](ctx context.Context, c *conn, req PDU, resp Response) error {
	// Send the request and
	// wait for the function by which we can read the response
	// (it comes from the receiveLoop calling receiveAndDispatch which reads the header)
	// The receiveLoop blocks read access to the net.Conn until the respFunc is executed
	select {
	case respFunc := <-c.sendRequest(req):
		// Fill it by using PDU.Read()
		return errtrace.Wrap(respFunc(resp))
	case <-ctx.Done():
		go func() {
			// The respFunc has to be executed in any case. Otherwise, the receiveLoop will block
			respFunc := <-c.sendRequest(req)
			_ = respFunc(resp)
		}()
		return errtrace.Wrap(ctx.Err())
	}
}

// sendRequest enqueues a request at the sendLoop.
// The sendLoop generates a transactionID for the request and sends it over the net.Conn.
// Then, the sendLoop stores request.ch under this transactionID.
// sendRequest returns request.ch to readResponse
func (c *conn) sendRequest(pdu PDU) <-chan func(PDU) error {
	req := request{
		write: func(conn io.Writer) (transactionId uint32, err error) {
			// Make sure header and PDU are sent in one package if possible
			mtuWriter := bufio.NewWriterSize(conn, 1500) // Ethernet MTU is 1500
			transactionId, err = c.writeHeader(mtuWriter, pdu)
			// log.Printf("sent Header %v, id: %v", pdu.MessageType(), transactionId)
			if err != nil {
				return transactionId, errtrace.Wrap(err)
			}
			err = pdu.Write(mtuWriter)
			if err == nil {
				err = mtuWriter.Flush()
			}
			return transactionId, errtrace.Wrap(err)
		},
		ch: make(chan func(PDU) error),
	}
	// Push the request to the sendloop
	if err := c.enqueueRequest(req); err != nil {
		// The sendLoop does not run anymore
		// Build an result chan which errors
		ch := make(chan func(PDU) error, 1)
		errFunc := func(PDU) error { return errtrace.Wrap(err) }
		ch <- errFunc
		return ch
	}
	return req.ch
}

func (c *conn) transactionAllowed() chan struct{} {
	c.mxState.RLock()
	defer c.mxState.RUnlock()

	return c.concurrentTransactionLimitCh
}

func (c *conn) transactionStarted() chan struct{} {
	c.mxState.RLock()
	defer c.mxState.RUnlock()

	return c.transactionStartedCh
}
