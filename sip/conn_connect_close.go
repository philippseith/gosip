package sip

import (
	"context"
	"time"

	"github.com/joomcode/errorx"
)

func (c *conn) connect(ctx context.Context) error {
	// TODO detect network latency and add it to the busy timeout
	c.timeoutReader.SetTimeout(time.Duration(c.userBusyTimeout) * time.Millisecond)
	select {
	case <-ctx.Done():
		return errorx.EnsureStackTrace(ctx.Err())
	case respFunc := <-c.sendRequest(&ConnectRequest{
		Version:      1,
		BusyTimeout:  c.userBusyTimeout,
		LeaseTimeout: c.userLeaseTimeout,
	}):
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
				go c.sendKeepAliveLoop(ctx)
			}
		}()
		return nil
	}
}

func (c *conn) sendKeepAliveLoop(ctx context.Context) {
	loopTime := c.LeaseTimeout() - 100*time.Millisecond
	<-time.After(loopTime)

	ctxDl, cancel := context.WithDeadline(ctx, time.Now().Add(loopTime))
	err := c.Ping(ctxDl)
	cancel()
	if err != nil {
		logger.Printf("%v: sendKeepAlive: %v", c.address, err)
		return
	}

	ticker := time.NewTicker(loopTime)
	defer ticker.Stop()

	for range ticker.C {
		ctxDl, cancel := context.WithDeadline(ctx, time.Now().Add(loopTime))
		err := c.Ping(ctxDl)
		cancel()
		if err != nil {
			logger.Printf("%v: sendKeepAlive: %v", c.address, err)
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
		if err != nil {
			errorx.EnsureStackTrace(err)
		}
		c.Conn = nil
	}
	return err
}
