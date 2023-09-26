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

func (c *Conn) reqChOut() <-chan request {
	c.mxState.RLock()
	defer c.mxState.RUnlock()

	if c.reqCh != nil {
		return c.reqCh
	}
	ch := make(chan request)
	close(ch)
	return ch
}

func (c *Conn) reqChIn() chan<- request {
	c.mxState.RLock()
	defer c.mxState.RUnlock()

	return c.reqCh
}

func (c *Conn) sendloop(ctx context.Context, cancel context.CancelCauseFunc) {
loop:
	for {
		select {
		case <-ctx.Done():
			log.Printf("breaking sendLoop: %v", context.Cause(ctx))
			break loop
		case req, ok := <-c.reqChOut():
			if !ok {
				err := errors.New("breaking sendLoop: reqCh closed")
				log.Print(err)
				cancel(err)
				break loop
			}
			if c.concurrentCh != nil {
				// Check if we could send. This blocks when no more concurrent requests are allowed
				c.concurrentCh <- struct{}{}
			}
			if err := c.send(req); err != nil {
				c.cancelAllRequests(err)
				log.Printf("breaking sendLoop: %v", err)
				cancel(err)
				break loop
			}
		}
	}
}

func (c *Conn) receiveLoop(ctx context.Context, cancel context.CancelCauseFunc) {
loop:
	for {
		select {
		case <-ctx.Done():
			log.Printf("breaking receiveLoop: %v", context.Cause(ctx))
			break loop
		default:
			// Wait for at least one
			if err := c.receive(); err != nil {
				c.cancelAllRequests(err)
				log.Printf("breaking receiveLoop: %v", err)
				cancel(err)
				break loop
			}
			// decrease the number of currently running req/resp pairs
			if c.concurrentCh != nil {
				<-c.concurrentCh
			}
		}
	}
}

func (c *Conn) cancelAllRequests(err error) {
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

func (c *Conn) send(req request) error {
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

func (c *Conn) receive() error {
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

func (c *Conn) newExceptionResponse(respFuncExecuted chan struct{}) (func(PDU) error, error) {
	ex := Exception{}
	if err := ex.Read(c.timeoutReader); err != nil {
		return nil, err
	}
	return func(PDU) error {
		defer close(respFuncExecuted)

		return ex
	}, nil
}

func (c *Conn) checkoutResponseChan(tID uint32) chan func(PDU) error {
	c.mxRC.Lock()
	defer c.mxRC.Unlock()

	ch := c.respChans[tID]
	delete(c.respChans, tID)
	return ch
}

func (c *Conn) writeHeader(conn io.Writer, pdu PDU) (transactiondID uint32, err error) {
	h := Header{
		TransactionID: atomic.AddUint32(&c.transactionID, 1),
		MessageType:   pdu.MessageType(),
	}
	return h.TransactionID, h.Write(conn)
}

func (c *Conn) sendWaitForResponse(pdu PDU) func(PDU) error {
	req := request{
		write: func(conn io.Writer) (transactionId uint32, err error) {
			// Make sure header and PDU are sent in one package if possible
			mtuWriter := bufio.NewWriterSize(conn, 1500) // Ethernet MTU is 1500
			transactionId, err = c.writeHeader(mtuWriter, pdu)
			if err != nil {
				return transactionId, err
			}
			err = pdu.Write(mtuWriter)
			mtuWriter.Flush()
			return transactionId, err
		},
		ch: make(chan func(PDU) error),
	}
	defer close(req.ch)
	// Get the sendLoop queue
	ch := c.reqChIn()
	// Is the connection closed?
	if ch == nil {
		return func(PDU) error { return ErrorClosed }
	}
	// Send request job into the queue of the sendLoop
	ch <- req
	// wait for the function by which we can read the response (comes from the receiveLoop which reads the header)
	readResp := <-req.ch
	return readResp
}
