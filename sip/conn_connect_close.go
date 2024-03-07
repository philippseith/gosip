package sip

import (
	"context"
	"time"

	"braces.dev/errtrace"
)

func (c *conn) connect() error {
	// TODO detect network latency and add it to the busy timeout
	c.timeoutReader.SetTimeout(time.Duration(c.userBusyTimeout) * time.Millisecond)
	respFunc := <-c.sendRequest(&ConnectRequest{
		Version:      1,
		BusyTimeout:  c.userBusyTimeout,
		LeaseTimeout: c.userLeaseTimeout,
	})
	respPdu := &ConnectResponse{}
	if err := respFunc(respPdu); err != nil {
		return errtrace.Wrap(err)
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

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(loopTime))
	err := c.Ping(ctx)
	cancel()
	if err != nil {
		logger.Printf("sendKeepAlive: %v", err)
		return
	}

	ticker := time.NewTicker(loopTime)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(loopTime))
		err := c.Ping(ctx)
		cancel()
		if err != nil {
			logger.Printf("sendKeepAlive: %v", err)
			break
		}
	}
}

func (c *conn) cancelAllRequests(err error) {
	c.mxRC.Lock()
	defer c.mxRC.Unlock()

	errFunc := func(PDU) error {
		return errtrace.Wrap(err)
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
		c.reqCh = nil
	}
	if c.transactionStartedCh != nil {
		c.transactionStartedCh = nil
	}
	if c.concurrentTransactionLimitCh != nil {
		c.concurrentTransactionLimitCh = nil
	}

	if c.Conn != nil {
		err = c.Conn.Close()
		c.Conn = nil
	}
	return errtrace.Wrap(err)
}
