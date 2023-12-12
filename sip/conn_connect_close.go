package sip

import (
	"log"
	"time"
)

func (c *conn) connect() error {
	// TODO detect network latency and add it to the busy timeout
	c.timeoutReader.SetTimeout(time.Duration(c.userBusyTimeout) * time.Millisecond)
	respFunc := <-c.sendAndWaitForResponse(&ConnectRequest{
		Version:      1,
		BusyTimeout:  c.userBusyTimeout,
		LeaseTimeout: c.userLeaseTimeout,
	})
	respPdu := &ConnectResponse{}
	if err := respFunc(respPdu); err != nil {
		return err
	}
	func() {
		c.mxCR.Lock()
		defer c.mxCR.Unlock()

		c.connectResponse = *respPdu
		c.timeoutReader.SetTimeout(time.Duration(c.connectResponse.BusyTimeout) * time.Millisecond)
		// Eventually start the KeepAlive loop
		if c.sendKeepAlive {
			go c.sendKeepAliveLoop()
		}
	}()
	return nil
}

func (c *conn) sendKeepAliveLoop() {
	loopTime := c.LeaseTimeout() - 100*time.Millisecond
	<-time.After(loopTime)
	if err := c.Ping(); err != nil {
		log.Printf("sendKeepAlive: %v", err)
		return
	}

	ticker := time.NewTicker(loopTime)
	defer ticker.Stop()

	for range ticker.C {
		if err := c.Ping(); err != nil {
			log.Printf("sendKeepAlive: %v", err)
			break
		}
	}
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

func (c *conn) cleanUp() (err error) {
	c.mxState.Lock()
	defer c.mxState.Unlock()

	if c.reqCh != nil {
		close(c.reqCh)
		c.reqCh = nil
	}
	if c.transactionStartedCh != nil {
		close(c.transactionStartedCh)
		c.transactionStartedCh = nil
	}
	if c.concurrentTransactionLimitCh != nil {
		close(c.concurrentTransactionLimitCh)
		c.concurrentTransactionLimitCh = nil
	}

	if c.Conn != nil {
		err = c.Conn.Close()
		c.Conn = nil
	}
	return err
}
