package sip

import (
	"context"
	"io"
	"net"

	"github.com/joomcode/errorx"
)

// Dial opens a Conn and connects it.
func Dial(network, address string, options ...ConnOption) (Conn, error) {
	c, err := dial(context.Background(), network, address, options...)
	// See https://www.reddit.com/r/golang/comments/1bu5r72/subtle_and_surprising_behavior_when_interface/
	// A nil reference to conn is not the same as a nil Conn and can not compared to nil if returned als Conn
	if c == nil {
		return nil, err
	}
	return c, err
}

func dial(ctx context.Context, network, address string, options ...ConnOption) (*conn, error) {

	netConn, err := dialNetConn(network, address, options...)
	if err != nil {
		return nil, err
	}

	c := &conn{
		Conn: netConn,
		connOptions: connOptions{
			userBusyTimeout:  2000,
			userLeaseTimeout: 10000,
		},
		address:       address,
		timeoutReader: &timeoutReader{reader: netConn},

		reqCh:                make(chan request),
		transactionStartedCh: make(chan struct{}, 10000), // Practically infinite queue size, no memory allocation because of struct{} type
		respChans:            map[uint32]chan func(PDU) error{},
	}

	// Default: Allow practically infinite parallel transactions
	_ = WithConcurrentTransactionLimit(5000)(&c.connOptions)
	// But what does the user want?
	for _, option := range options {
		if err := option(&c.connOptions); err != nil {
			return nil, errorx.EnsureStackTrace(err)
		}
	}

	// Prepare corking
	if c.corkInterval != 0 {

		var corkCtx context.Context
		corkCtx, c.cancel = context.WithCancelCause(ctx)

		c.writer, err = newCorkWriter(corkCtx, c.Conn, c.corkInterval, c.transactionStarted)
		if err != nil {
			logger.Printf("can not init corking: %v", err)
		}
	}
	// No corking, but make sure header and request are send in one datagram
	if c.writer == nil {
		c.writer = newSingleTransactionWriter(c.Conn, c.transactionStarted)
	}

	// we use userBusy as BusyTimeout until the server responded
	c.connectResponse.BusyTimeout = c.userBusyTimeout

	sendRecvCtx, cancel := context.WithCancelCause(ctx)

	go c.sendLoop(sendRecvCtx, cancel)
	go c.receiveLoop(sendRecvCtx, cancel)
	go func() {
		<-sendRecvCtx.Done()
		c.cancelAllRequests(errorx.EnsureStackTrace(context.Cause(sendRecvCtx)))
		c.setClosed()
	}()

	return c, c.connect(ctx)
}

// dialNetConn checks for WithDial option (fallback to net.Dial) and dials the connection
func dialNetConn(network, address string, options ...ConnOption) (io.ReadWriteCloser, error) {
	wcOpts := &connOptions{}
	for _, option := range options {
		if err := option(wcOpts); err != nil {
			return nil, errorx.EnsureStackTrace(err)
		}
	}
	// Fallback to net.Dial
	if wcOpts.dial == nil {
		wcOpts.dial = func(network, address string) (io.ReadWriteCloser, error) {
			netConn, err := net.Dial(network, address)
			if err != nil {
				err = errorx.EnsureStackTrace(err)
			}
			return netConn, err
		}
	}
	return wcOpts.dial(network, address)
}
