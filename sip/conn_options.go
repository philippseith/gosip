package sip

import (
	"fmt"
	"io"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/joomcode/errorx"
)

// ConnOption is option for Conn
type ConnOption func(c *connOptions) error

type connOptions struct {
	dial                         func(string, string) (io.ReadWriteCloser, error)
	corkInterval                 time.Duration
	userBusyTimeout              uint32
	userLeaseTimeout             uint32
	concurrentTransactionLimitCh chan struct{}
	sendKeepAlive                bool
	backoffFactory               func() backoff.BackOff
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
func WithDial(dial func(string, string) (io.ReadWriteCloser, error)) ConnOption {
	return func(c *connOptions) error {
		c.dial = dial
		return nil
	}
}

func WithCorking(corkInterval time.Duration) ConnOption {
	return func(c *connOptions) error {
		c.corkInterval = corkInterval
		return nil
	}
}
