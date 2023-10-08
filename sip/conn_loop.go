package sip

import (
	"context"
	"log"
	"net"
	"sync"
	"time"
)

func (c *Conn) connLoop(network, address string) chan error {
	var ctx context.Context
	ctx, c.cancel = context.WithCancelCause(context.Background())

	ch := make(chan error, 1)
	go func(ch chan error) {
		defer close(ch)

		initial := true
	loop:
		for {
			log.Print("conn_loop")
			select {
			case <-ctx.Done():
				log.Printf("breaking connLoop: %v", context.Cause(ctx))
				break loop
			default:
				sendRecvCtx, wg, err := c.connect(ctx, network, address)
				if initial {
					initial = false
					ch <- err
				}
				if err != nil {
					log.Printf("breaking connLoop: %v", err)
					c.cancel(err)
					break loop
				}
				<-sendRecvCtx.Done()
				wg.Wait()
			}
		}
		ch <- c.cleanUp()

	}(ch)
	return ch
}

func (c *Conn) connect(ctx context.Context, network, address string) (context.Context, *sync.WaitGroup, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, nil, err
	}

	func() {
		c.mxState.Lock()
		defer c.mxState.Unlock()

		c.Conn = conn
		c.timeoutReader.reader = conn
		c.reqCh = make(chan request)
		c.respChans = map[uint32]chan func(PDU) error{}
	}()

	sendRecvCtx, sendRecvCancel := context.WithCancelCause(ctx)
	sendRecvWg := &sync.WaitGroup{}
	sendRecvWg.Add(2)
	go c.sendLoop(sendRecvCtx, sendRecvCancel, sendRecvWg)
	go c.receiveLoop(sendRecvCtx, sendRecvCancel, sendRecvWg)

	// TODO detect network latency and add it to the busy timeout

	return sendRecvCtx, sendRecvWg, c.sendReceiveConnect()
}

func (c *Conn) sendReceiveConnect() error {
	c.timeoutReader.SetTimeout(time.Duration(c.userBusyTimeout) * time.Millisecond)
	readResponse := c.sendWaitForResponse(&ConnectRequest{
		Version:      1,
		BusyTimeout:  c.userBusyTimeout,
		LeaseTimeout: c.userLeaseTimeout,
	})
	respPdu := &ConnectResponse{}
	if err := readResponse(respPdu); err != nil {
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

func (c *Conn) sendKeepAliveLoop() {
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
