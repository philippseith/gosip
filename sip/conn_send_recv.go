package sip

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync/atomic"
)

func (c *conn) nextRequest() <-chan request {
	c.mxState.RLock()
	defer c.mxState.RUnlock()

	if c.reqCh != nil {
		return c.reqCh
	}
	// Connection closed, return closed channel
	ch := make(chan request)
	close(ch)
	return ch
}

func (c *conn) sendLoop(ctx context.Context, cancel context.CancelCauseFunc) {
	var err error
loop:
	for {
		select {
		case <-ctx.Done():
			err = context.Cause(ctx)
			break loop
		case req, ok := <-c.nextRequest():
			if !ok {
				err = errors.New("reqCh closed")
				cancel(err)
				break loop
			}
			if err = c.waitForConcurrentTransactionAllowed(); err != nil {
				break
			}
			if err = c.send(req); err != nil {
				c.cancelAllRequests(err)
				cancel(err)
				break loop
			}
			// Inform receiveLoop there's a new transaction initiated
			if err = c.signalTransactionStarted(); err != nil {
				break
			}
		}
	}
	log.Printf("breaking sendLoop: %v", err)
}

func (c *conn) receiveLoop(ctx context.Context, cancel context.CancelCauseFunc) {
	var err error
loop:
	for {
		select {
		case <-ctx.Done():
			err = context.Cause(ctx)
			break loop
		default:
			// Wait for an initiated transaction
			if err = c.waitForTransactionStarted(); err != nil {
				break loop
			}
			if err = c.receive(); err != nil {
				c.cancelAllRequests(err)
				cancel(err)
				break loop
			}
			// decrease the number of currently running req/resp pairs
			if err = c.signalConcurrentTransactionAllowed(); err != nil {
				break loop
			}
		}
	}
	log.Printf("breaking receiveLoop: %v", err)
}

func (c *conn) signalConcurrentTransactionAllowed() error {
	return signal(c.getConcurrentTransactionLimitCh)
}

func (c *conn) waitForConcurrentTransactionAllowed() error {
	return wait(c.getConcurrentTransactionLimitCh)
}

func (c *conn) signalTransactionStarted() error {
	return signal(c.getTransactionStartedCh)
}

func (c *conn) waitForTransactionStarted() error {
	return wait(c.getTransactionStartedCh)
}

func (c *conn) cancelAllRequests(err error) {
	c.mxRC.Lock()
	defer c.mxRC.Unlock()

	errFunc := func(PDU) error {
		return err
	}
	for _, ch := range c.respChans {
		cch := ch
		go func() { cch <- errFunc }()
	}
	c.respChans = map[uint32]chan func(PDU) error{}
}

func (c *conn) send(req request) error {
	tID, err := req.write(c.Conn)
	if err != nil {
		return err
	}
	func() {
		c.mxRC.Lock()
		defer c.mxRC.Unlock()

		c.respChans[tID] = req.ch
	}()
	return nil
}

func (c *conn) receive() error {
	c.mxRecv.Lock()
	defer c.mxRecv.Unlock()

	h := &Header{}
	err := h.Read(c.timeoutReader)
	if err != nil {
		return err
	}
	if h.MessageType == 0 {
		return nil
	}
	var respFunc func(PDU) error
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
			return pdu.Read(c.timeoutReader)
		}
	}
	c.checkoutResponseChan(h.TransactionID) <- respFunc
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

func (c *conn) sendAndWaitForResponse(pdu PDU) func(PDU) error {
	req := request{
		write: func(conn io.Writer) (transactionId uint32, err error) {
			// Make sure header and PDU are sent in one package if possible
			mtuWriter := bufio.NewWriterSize(conn, 1500) // Ethernet MTU is 1500
			transactionId, err = c.writeHeader(mtuWriter, pdu)
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
	defer close(req.ch)
	// Push the request to the sendloop
	if err := c.pushToSendLoop(req); err != nil {
		// The sendloop does not run anymore
		return func(PDU) error { return err }
	}
	// wait for the function by which we can read the response (comes from the receiveLoop which reads the header)
	readResp := <-req.ch
	// When this returns, req.ch can be closed
	return readResp
}

func (c *conn) pushToSendLoop(req request) error {
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

func (c *conn) getConcurrentTransactionLimitCh() chan struct{} {
	c.mxState.Lock()
	defer c.mxState.Unlock()

	return c.concurrentTransactionLimitCh
}

func (c *conn) getTransactionStartedCh() chan struct{} {
	c.mxState.Lock()
	defer c.mxState.Unlock()

	return c.transactionStartedCh
}

func signal(getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return ErrorClosed
	}
	_, ok := <-ch
	if !ok {
		return ErrorClosed
	}
	return nil
}

func wait(getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return ErrorClosed
	}
	ch <- struct{}{}
	return nil
}
