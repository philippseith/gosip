package sip

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"sync/atomic"
	"time"
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
				return context.Cause(ctx)
			// Get a new request
			case req, ok := <-c.dequeueRequest():
				if !ok {
					return ErrorClosed
				}
				if err := wait(c.transactionAllowed); err != nil {
					return err
				}
				if err := c.send(req); err != nil {
					cancel(err)
					return err
				}
				// Inform receiveLoop there's a new transaction initiated
				if err := signal(c.transactionStarted); err != nil {
					return err
				}
			}
		}
	}()
	log.Printf("breaking sendLoop: %v", err)
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
				return context.Cause(ctx)
			default:
				// Wait for an initiated transaction
				if err := wait(c.transactionStarted); err != nil {
					return err
				}
				if err := c.receiveAndDispatch(); err != nil {
					cancel(err)
					return err
				}
				// decrease the number of currently running req/resp pairs
				if err := signal(c.transactionAllowed); err != nil {
					return err
				}
			}
		}
	}()
	log.Printf("breaking receiveLoop: %v", err)
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
		return ErrorClosed
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
		return err
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

	h := &Header{}
	err := h.Read(c.timeoutReader)
	if err != nil {
		return err
	}
	if h.MessageType == 0 { // TODO When does this happen?
		return nil
	}
	// The header was read, this is the first point in time we can be sure the server has sent something
	c.setLastReceived()
	// The respFunc is executed on the receiving goroutine
	var respFunc func(PDU) error
	// prepare waiting for the respFunc to end
	respFuncExecuted := make(chan struct{})
	switch h.MessageType {
	case BusyResponseMsgType:
		// Busy PDU is empty, do nothing
		return nil
	case ExceptionMsgType:
		respFunc, err = c.newExceptionResponse(respFuncExecuted)
		if err != nil {
			return err
		}
	default:
		respFunc = func(pdu PDU) error {
			defer close(respFuncExecuted)

			if h.MessageType != pdu.MessageType() {
				return fmt.Errorf(
					"%w. Type %d, Expected: %d, TransactionId: %d",
					ErrorInvalidResponseMessageType,
					h.MessageType, pdu.MessageType(), h.TransactionID)
			}
			// log.Printf("receiving %v, id: %v", pdu.MessageType(), h.TransactionID)
			err = pdu.Read(c.timeoutReader)
			// log.Printf("received %v, id: %v, err: %v", pdu.MessageType(), h.TransactionID, err)
			return err
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
		return nil, err
	}
	return func(PDU) error {
		defer close(respFuncExecuted)

		return ex
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
	return h.TransactionID, h.Write(conn)
}

func readResponse[Request RequestPDU, Response PDU](c *conn, slaveIndex, slaveExtension int, idn uint32, ch chan Result[Response]) {
	req := *new(Request)
	req.Init(slaveIndex, slaveExtension, idn)
	respFunc := <-c.sendAndWaitForResponse(req)
	resp := new(Response)
	err := respFunc(*resp)
	if err != nil {
		ch <- Err[Response](err)
	} else {
		ch <- Ok[Response](*resp)
	}
	close(ch)
}

func (c *conn) sendAndWaitForResponse(pdu PDU) <-chan func(PDU) error {
	req := request{
		write: func(conn io.Writer) (transactionId uint32, err error) {
			// Make sure header and PDU are sent in one package if possible
			mtuWriter := bufio.NewWriterSize(conn, 1500) // Ethernet MTU is 1500
			transactionId, err = c.writeHeader(mtuWriter, pdu)
			// log.Printf("sent Header %v, id: %v", pdu.MessageType(), transactionId)
			if err != nil {
				return transactionId, err
			}
			err = pdu.Write(mtuWriter)
			if err == nil {
				err = mtuWriter.Flush()
			}
			return transactionId, err
		},
		ch: make(chan func(PDU) error),
	}
	// Push the request to the sendloop
	if err := c.enqueueRequest(req); err != nil {
		// The sendLoop does not run anymore
		// Build an result chan which errors
		ch := make(chan func(PDU) error, 1)
		err := func(PDU) error { return err }
		ch <- err
		return ch
	}
	// wait for the function by which we can read the response
	// (comes from the receiveLoop calling receiveAndDispatch which reads the header)
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
