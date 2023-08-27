package sip

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
)

type Conn struct {
	net.Conn
	mxRecv sync.Mutex

	transactionId uint32

	reqCh        chan request
	concurrentCh chan struct{}

	respChans map[uint32]chan func(PDU) (Exception, error)
	mxRC      sync.Mutex

	connectResponse ConnectResponse
	mxCR            sync.RWMutex
}

type request struct {
	write func(conn io.Writer) (transactionId uint32, err error)
	ch    chan func(PDU) (Exception, error)
}

func (c *Conn) sendloop() {
	for req := range c.reqCh {
		if c.concurrentCh != nil {
			c.concurrentCh <- struct{}{}
		}
		if err := c.send(req); err != nil {
			c.cancelAllRequests(err)
			log.Printf("breaking sendLoop: %v", err)
			break
		}
	}
}

func (c *Conn) receiveLoop() {
	for {
		if err := c.receive(); err != nil {
			c.cancelAllRequests(err)
			log.Printf("breaking receiveLoop: %v", err)
			break
		}
		if c.concurrentCh != nil {
			<-c.concurrentCh
		}
	}
}

func (c *Conn) cancelAllRequests(err error) {
	errFunc := func(PDU) (Exception, error) {
		return Exception{}, err
	}
	for _, ch := range c.respChans {
		cch := ch
		go func() { cch <- errFunc }()
	}
	c.respChans = map[uint32]chan func(PDU) (Exception, error){}
}

func (c *Conn) send(req request) error {
	tId, err := req.write(c.Conn)
	if err != nil {
		return err
	}
	func() {
		c.mxRC.Lock()
		defer c.mxRC.Unlock()

		c.respChans[tId] = req.ch
	}()
	return nil
}

func (c *Conn) receive() error {
	c.mxRecv.Lock()
	defer c.mxRecv.Unlock()

	h := &Header{}
	err := h.Read(c.Conn)
	if h.MessageType == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	var respFunc func(PDU) (Exception, error)
	respFuncExecuted := make(chan struct{})
	if h.MessageType == ExceptionMsgType {
		respFunc, err = c.newExceptionResponse(respFuncExecuted)
		if err != nil {
			return err
		}
	} else {
		respFunc = func(pdu PDU) (ex Exception, err error) {
			defer close(respFuncExecuted)

			if h.MessageType != pdu.MessageType() {
				return ex, fmt.Errorf(
					"invalid response message type %d. Expected: %d. TransactionId: %d",
					h.MessageType, pdu.MessageType(), h.TransactionID)
			}
			return ex, pdu.Read(c.Conn)
		}
	}
	c.checkoutResponseChan(h.TransactionID) <- respFunc
	<-respFuncExecuted
	return nil
}

func (c *Conn) newExceptionResponse(respFuncExecuted chan struct{}) (func(PDU) (Exception, error), error) {
	ex := Exception{}
	if err := ex.Read(c.Conn); err != nil {
		return nil, err
	}
	return func(PDU) (Exception, error) {
		defer close(respFuncExecuted)

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
		ch: make(chan func(PDU) (Exception, error)),
	}
	defer close(req.ch)
	// Send request job into the queue
	c.reqCh <- req
	// wait for the function by which we can read the response
	readResp := <-req.ch
	return readResp
}
