package sip

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/joomcode/errorx"
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
				return errorx.EnsureStackTrace(context.Cause(ctx))
			// Get a new request
			case req, ok := <-c.dequeueRequest():
				if !ok {
					return errorx.EnsureStackTrace(ErrorClosed)
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
	logger.Printf("%s: breaking sendLoop: %v", c.address, err)
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
				return errorx.EnsureStackTrace(context.Cause(ctx))
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
	logger.Printf("%s: breaking receiveLoop: %v", c.address, err)
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
		return errorx.EnsureStackTrace(ErrorClosed)
	}
	// Send request job into the queue of the sendLoop
	atomic.AddInt32(&c.reqChWaitCount, 1)
	ch <- req
	atomic.AddInt32(&c.reqChWaitCount, -1)
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
	if !req.deadline.IsZero() {
		// Register the per-request deadline so that readNextResponse can widen
		// the timeoutReader's effective timeout beyond the connection-level
		// BusyTimeout when necessary. Without this, a per-request timeout that
		// is longer than BusyTimeout would never fire: the timeoutReader would
		// kill the connection first.
		c.mxRD.Lock()
		if c.reqDeadlines == nil {
			c.reqDeadlines = make(map[uint32]time.Time)
		}
		c.reqDeadlines[transactionID] = req.deadline
		c.mxRD.Unlock()
	}
	return nil
}

// receiveAndDispatch reads from the net.Conn and dispatches
// the responses according to the received transactionIDs.
// receiveAndDispatch lives in the receiveLoop.
func (c *conn) receiveAndDispatch() error {
	c.mxRecv.Lock()
	defer c.mxRecv.Unlock()

	// Save the connection-level timeout and restore it when done, so that
	// a per-request override does not bleed into subsequent calls.
	connTimeout := c.timeoutReader.Timeout()
	defer c.timeoutReader.SetTimeout(connTimeout)

	respFuncExecuted := make(chan struct{})
	h, respFunc, err := c.readNextResponse(connTimeout, respFuncExecuted)
	if err != nil {
		return err
	}
	// Get the response channel of the request for this transactionID and send the respFunc to it
	ch := c.checkoutResponseChan(h.TransactionID)
	if ch == nil {
		return errorx.EnsureStackTrace(fmt.Errorf("%w: received response for unknown transaction ID %d", Error, h.TransactionID))
	}
	ch <- respFunc
	// Important: Wait for the current respFunc to read the rest of the message (the PDU) from the net.Conn
	<-respFuncExecuted
	return nil
}

// readNextResponse loops until a non-Busy response header is received, then
// returns the header and a function that reads the rest of the PDU.
//
// # Timeout handling
//
// Two timeout layers are in play:
//
//  1. Connection-level timeout (connTimeout): the BusyTimeout negotiated with
//     the server. This is the baseline — the server guarantees a reply or a
//     Busy PDU within this window.
//
//  2. Per-request deadline: stored in c.reqDeadlines by send() for any request
//     whose context carries a deadline. Overrides connTimeout in both directions
//     (shorter OR longer).
//
// Before every header read, maxRemainingDeadline raises the effective timeout
// to the longest active per-request deadline, so a slow server does not kill
// the connection while a long-timeout request is still pending.
//
// Once the header has been read (and the transaction ID is known), popRequestDeadline
// narrows the timeout back to that specific request's remaining deadline before
// the PDU body is read. Because servers send header and body together, this
// narrowing only matters when the per-request timeout is shorter than connTimeout.
func (c *conn) readNextResponse(connTimeout time.Duration, respFuncExecuted chan struct{}) (*Header, func(PDU) error, error) {
	h := &Header{}
	for {
		// Apply the most generous active per-request deadline before reading the
		// header: servers typically send header and PDU in one step, so the same
		// timeout should cover both.
		c.timeoutReader.SetTimeout(c.maxRemainingDeadline(connTimeout))
		if err := h.Read(c.timeoutReader); err != nil {
			return nil, nil, err
		}
		// The header was read, this is the first point in time we can be sure the server has sent something
		c.setLastReceived()
		switch h.MessageType {
		case 0:
			return nil, nil, errorx.EnsureStackTrace(fmt.Errorf(
				"%w: received message with invalid type 0, transactionId: %d",
				ErrorInvalidResponseMessageType, h.TransactionID))
		case BusyResponseMsgType:
			// Busy PDU is empty, do nothing and wait for the real response.
			// Do not consume the deadline; it will be re-applied on the next iteration.
			continue
		case ExceptionMsgType:
			// Narrow to this request's remaining deadline before reading the body.
			if remaining := c.popRequestDeadline(h.TransactionID); remaining > 0 {
				c.timeoutReader.SetTimeout(remaining)
			}
			respFunc, err := c.newExceptionResponse(respFuncExecuted)
			if err != nil {
				return nil, nil, err
			}
			return h, respFunc, nil
		default:
			// Narrow to this request's remaining deadline before reading the body.
			if remaining := c.popRequestDeadline(h.TransactionID); remaining > 0 {
				c.timeoutReader.SetTimeout(remaining)
			}
			return h, c.buildPDUResponseFunc(h, respFuncExecuted), nil
		}
	}
}

// buildPDUResponseFunc returns the function that reads the PDU body for a standard response.
func (c *conn) buildPDUResponseFunc(h *Header, done chan struct{}) func(PDU) error {
	return func(pdu PDU) error {
		defer close(done)
		if h.MessageType != pdu.MessageType() {
			return errorx.EnsureStackTrace(fmt.Errorf(
				"%w. Type %d, Expected: %d, TransactionId: %d",
				ErrorInvalidResponseMessageType,
				h.MessageType, pdu.MessageType(), h.TransactionID))
		}
		if err := pdu.Read(c.timeoutReader); err != nil {
			return errorx.Decorate(err, "received %v, id: %v", pdu.MessageType(), h.TransactionID)
		}
		return nil
	}
}

func (c *conn) newExceptionResponse(respFuncExecuted chan struct{}) (func(PDU) error, error) {
	ex := Exception{}
	if err := ex.Read(c.timeoutReader); err != nil {
		return nil, err
	}
	return func(PDU) error {
		defer close(respFuncExecuted)

		return errorx.EnsureStackTrace(ex)
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

// popRequestDeadline removes the per-request deadline for tID from the map and
// returns the remaining time until it. Returns 0 if no deadline is registered.
// Removing the entry on first use ensures a Busy-response loop does not
// re-apply the deadline after it has already been consumed for the body read.
func (c *conn) popRequestDeadline(tID uint32) time.Duration {
	c.mxRD.Lock()
	defer c.mxRD.Unlock()

	dl, ok := c.reqDeadlines[tID]
	if !ok {
		return 0
	}
	delete(c.reqDeadlines, tID)
	return time.Until(dl)
}

// maxRemainingDeadline returns the maximum remaining time across all registered
// per-request deadlines, or fallback if none are registered or all have expired.
// Using the maximum ensures the connection is kept alive long enough for the
// slowest pending request; once all long-timeout requests finish, the timeout
// reverts to the connection-level fallback.
func (c *conn) maxRemainingDeadline(fallback time.Duration) time.Duration {
	c.mxRD.Lock()
	defer c.mxRD.Unlock()

	result := fallback
	now := time.Now()
	for _, dl := range c.reqDeadlines {
		if remaining := dl.Sub(now); remaining > result {
			result = remaining
		}
	}
	return result
}

func (c *conn) writeHeader(conn io.Writer, pdu PDU) (transactionID uint32, err error) {
	h := Header{
		TransactionID: atomic.AddUint32(&c.transactionID, 1),
		MessageType:   pdu.MessageType(),
	}
	if err := h.Write(conn); err != nil {
		return h.TransactionID, errorx.EnsureStackTrace(err)
	}
	return h.TransactionID, nil
}

func sendRequestWaitForResponseAndRead[Response PDU](ctx context.Context, c *conn, req PDU, resp Response) error {
	// Send the request and
	// wait for the function by which we can read the response
	// (it comes from the receiveLoop calling receiveAndDispatch which reads the header)
	// The receiveLoop blocks read access to the net.Conn until the respFunc is executed
	select {
	case respFunc := <-c.sendRequest(ctx, req):
		// Fill it by using PDU.Read()
		return respFunc(resp)
	case <-ctx.Done():
		// Capture closedCh before spawning the goroutine. cleanUp nils c.closedCh
		// after closing it; selecting on a nil channel blocks forever, but selecting
		// on a closed channel returns immediately — we need the latter.
		closedCh := c.closedCh
		go func() {
			// The respFunc has to be executed in any case. Otherwise, the receiveLoop will block.
			// Guard with closedCh so we don't leak if the connection closes before the send
			// loop picks up the request.
			select {
			case respFunc := <-c.sendRequest(context.WithoutCancel(ctx), req):
				_ = respFunc(resp)
			case <-closedCh:
			}
		}()
		return errorx.EnsureStackTrace(ctx.Err())
	}
}

// sendRequest enqueues a request at the sendLoop.
// The sendLoop generates a transactionID for the request and sends it over the net.Conn.
// Then, the sendLoop stores request.ch under this transactionID.
// sendRequest returns request.ch to readResponse
func (c *conn) sendRequest(ctx context.Context, pdu PDU) <-chan func(PDU) error {
	deadline, _ := ctx.Deadline()
	req := request{
		write: func(conn io.Writer) (transactionId uint32, err error) {
			// Make sure header and PDU are sent in one package if possible
			bufferedWriter := bufio.NewWriterSize(conn, 1500) // Buffer header + PDU to reduce syscalls; S/IP writes are mostly small
			transactionId, err = c.writeHeader(bufferedWriter, pdu)
			// log.Printf("sent Header %v, id: %v", pdu.MessageType(), transactionId)
			if err != nil {
				return transactionId, err
			}
			err = pdu.Write(bufferedWriter)
			if err == nil {
				err = bufferedWriter.Flush()
			}
			return transactionId, err
		},
		ch:       make(chan func(PDU) error, 1),
		deadline: deadline,
	}
	// Push the request to the sendloop
	if err := c.enqueueRequest(req); err != nil {
		// The sendLoop does not run anymore
		// Build an result chan which errors
		ch := make(chan func(PDU) error, 1)
		errFunc := func(PDU) error { return err }
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
