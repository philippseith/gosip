package sip

import (
	"context"
	"fmt"
	"io"

	"github.com/cenkalti/backoff/v4"
	"github.com/joomcode/errorx"
)

// ConnOption is option for Conn
type ConnOption func(c *connOptions) error

type connOptions struct {
	dial                         func(context.Context, string, string) (io.ReadWriteCloser, error)
	dialCtx                      context.Context
	userBusyTimeout              uint32
	userLeaseTimeout             uint32
	concurrentTransactionLimitCh chan struct{}
	maxConnectionsCh             chan struct{}
	sendKeepAlive                bool
	backoffFactory               func() backoff.BackOff
	writerFactory                func(io.Writer) io.Writer
}

// WithDialContext sets the context for the dial function. This context is not used after the connection is established
// and is only used for the initial dial. If not set, the default context.Background() is used.
func WithDialContext(ctx context.Context) ConnOption {
	return func(c *connOptions) error {
		c.dialCtx = ctx
		return nil
	}
}

// WithBusyTimeout sets the BusyTimeout to negotiate with the server in ms. Default is 2000ms.
func WithBusyTimeout(timeout int) ConnOption {
	return func(c *connOptions) error {
		if timeout > 0 && timeout < int(^uint32(0)) {
			c.userBusyTimeout = uint32(timeout)
			return nil
		}
		return errorx.EnsureStackTrace(fmt.Errorf("%w: Timeout must be greater 0 and smaller %v", Error, ^uint32(0)))
	}
}

// WithLeaseTimeout sets the LeaseTimeout to negotiate with the server in ms. Default is 10000ms.
func WithLeaseTimeout(timeout int) ConnOption {
	return func(c *connOptions) error {
		if timeout > 0 && timeout < int(^uint32(0)) {
			c.userLeaseTimeout = uint32(timeout)
			return nil
		}
		return errorx.EnsureStackTrace(fmt.Errorf("%w: Timeout must be greater 0 and smaller %v", Error, ^uint32(0)))
	}
}

// WithMaxConnections limits the number of concurrently active server connections
// accepted by Serve. When the limit is reached the accept loop blocks until an
// existing connection closes, providing back-pressure at the TCP level.
// If not set, there is no limit. Must be greater than 0.
func WithMaxConnections(n int) ConnOption {
	return func(c *connOptions) error {
		if n <= 0 {
			return errorx.EnsureStackTrace(fmt.Errorf("%w: MaxConnections must be greater than 0", Error))
		}
		c.maxConnectionsCh = make(chan struct{}, n)
		return nil
	}
}

// WithConcurrentTransactionLimit limits the number of concurrent requests sent.
// If the option is not given in Dial, the concurrency is not limited.
func WithConcurrentTransactionLimit(ct uint) ConnOption {
	return func(c *connOptions) error {
		c.concurrentTransactionLimitCh = make(chan struct{}, ct)
		for i := uint(0); i < ct; i++ {
			c.concurrentTransactionLimitCh <- struct{}{}
		}
		return nil
	}
}

// WithSendKeepAlive configures the connection that it is sending Ping requests
// shortly before the LeaseTimeout ends.
func WithSendKeepAlive() ConnOption {
	return func(c *connOptions) error {
		c.sendKeepAlive = true
		return nil
	}
}

// WithMeasureNetworkLatencyICMP measures the network latency with an ICMP ping.
// If not set, the network latency is measured with S/IP Ping, which might lead
// to different latency results, depending on the server implementation.
// Note that ICMP ping requires specific system config options mentioned here:
// https://github.com/prometheus-community/#supported-operating-systems
func WithMeasureNetworkLatencyICMP() ConnOption {
	// TODO
	return func(c *connOptions) error { return nil } // nolint:revive
}

// WithDial surpasses the net.Conn from the Dial function.
// This option can be used for testing, logging, middleware purposes in general,
// or exotic connection types.
func WithDial(dial func(context.Context, string, string) (io.ReadWriteCloser, error)) ConnOption {
	return func(c *connOptions) error {
		c.dial = dial
		return nil
	}
}

// WithWriter allows to control the way requests are sent to the net.Conn.
// This can be useful to group request and/or send them at defined points in time.
func WithWriter(writerFactory func(io.Writer) io.Writer) ConnOption {
	return func(c *connOptions) error {
		c.writerFactory = writerFactory
		return nil
	}
}

// WithDialBackoff configures the backoff strategy for failed connects. See
// https://pkg.go.dev/github.com/cenkalti/backoff/v4 for more information about
// backoff strategies. Default is an exponential backoff, starting with a
// backoff time of 500ms, exponentially incremented by factor 1.5, with an
// overall timeout of 10 seconds.
func WithDialBackoff(backoffFactory func() backoff.BackOff) ConnOption {
	return func(c *connOptions) error {
		c.backoffFactory = backoffFactory
		return nil
	}
}
