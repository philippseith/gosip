package sip

import (
	"fmt"
	"io"
	"sync/atomic"
)

type request struct {
	write func(conn io.Writer) (transactionId uint32, err error)
	ch    chan chan func(PDU) (Exception, error)
}

func (c *Conn) sendloop() {
	for req := range c.reqCh {
		if err := c.send(req); err != nil {
			// decide if connection was lost
		}
	}
}

func (c *Conn) receiveLoop() {
	for {
		if err := c.receive(); err != nil {
			// decide if connection was lost
		}
	}
}

func (c *Conn) send(req request) error {
	tId, err := func() (uint32, error) {
		c.mxConn.Lock()
		defer c.mxConn.Unlock()

		return req.write(c.Conn)
	}()
	if err != nil {
		return err
	}
	func() {
		c.mxRC.Lock()
		defer c.mxRC.Unlock()

		c.respChans[tId] = make(chan func(PDU) (Exception, error), 1)
	}()
	return nil
}

func (c *Conn) receive() error {
	h := &Header{}
	err := h.Read(c.Conn)
	if err != nil {
		return err
	}
	var respFunc func(PDU) (Exception, error)
	if h.MessageType == ExceptionMsgType {
		respFunc, err = c.newExceptionResponse()
		if err != nil {
			return err
		}
	} else {
		respFunc = func(pdu PDU) (ex Exception, err error) {
			c.mxConn.Lock()
			defer c.mxConn.Unlock()

			if h.MessageType != pdu.MessageType() {
				return ex, fmt.Errorf(
					"invalid response message type %d. Expected: %d. TransactionId: %d",
					h.MessageType, pdu.MessageType(), h.TransactionID)
			}
			return ex, pdu.Read(c.Conn)
		}
	}
	ch := c.checkoutResponseChan(h.TransactionID)
	ch <- respFunc
	close(ch)
	return nil
}

func (c *Conn) newExceptionResponse() (func(PDU) (Exception, error), error) {
	ex := Exception{}
	if err := func() error {
		c.mxConn.Lock()
		defer c.mxConn.Unlock()

		return ex.Read(c.Conn)
	}(); err != nil {
		return nil, err
	}
	return func(PDU) (Exception, error) {
		return ex, nil
	}, nil
}

func (c *Conn) checkoutResponseChan(tId uint32) chan func(PDU) (Exception, error) {
	c.mxRC.Lock()
	defer c.mxRC.Unlock()

	ch := c.respChans[tId]
	delete(c.respChans, tId)
	return ch
}

func (c *Conn) writeHeader(conn io.Writer, pdu PDU) (transactiondId uint32, err error) {
	h := Header{
		TransactionID: atomic.AddUint32(&c.transactionId, 1),
		MessageType:   pdu.MessageType(),
	}
	return h.TransactionID, h.Write(conn)
}

func (c *Conn) sendWaitForResponse(pdu PDU) func(PDU) (Exception, error) {
	req := request{
		write: func(conn io.Writer) (transactionId uint32, err error) {
			transactionId, err = c.writeHeader(conn, pdu)
			if err != nil {
				return transactionId, err
			}
			return transactionId, pdu.Write(conn)
		},
		ch: make(chan chan func(PDU) (Exception, error)),
	}
	// Send request job into the queue
	c.reqCh <- req
	// get the channel were the function for reading the response comes in
	respCh := <-req.ch
	// wait for the function to read the response
	return <-respCh
}
