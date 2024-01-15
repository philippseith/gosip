package sip

import (
	"fmt"

	"braces.dev/errtrace"
	"github.com/cenkalti/backoff/v4"
)

// ConnOption is option for Conn
type ConnOption func(c *connOptions) error

type connOptions struct {
	userBusyTimeout              uint32
	userLeaseTimeout             uint32
	concurrentTransactionLimitCh chan struct{}
	sendKeepAlive                bool
	backoffFactory               func() backoff.BackOff
}

// WithBusyTimeout sets the BusyTimeout to negotiate with the server in ms. Default is 2000ms.
func WithBusyTimeout(timeout int) ConnOption {
	return func(c *connOptions) error {
		if timeout > 0 {
			c.userBusyTimeout = uint32(timeout)
		}
		return errtrace.Wrap(fmt.Errorf("%w: Timeout must be greater 0", Error))
	}
}

// WithLeaseTimeout sets the LeaseTimeout to negotiate with the server in ms. Default is 10000ms.
func WithLeaseTimeout(timeout int) ConnOption {
	return func(c *connOptions) error {
		if timeout > 0 {
			c.userLeaseTimeout = uint32(timeout)
		}
		return errtrace.Wrap(fmt.Errorf("%w: Timeout must be greater 0", Error))
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
func WithSendKeepAlive() func(c *connOptions) error {
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
	return func(c *connOptions) error { return nil }
}
