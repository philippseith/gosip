package sip

import (
	"fmt"

	"github.com/cenkalti/backoff/v4"
)

type connOptions struct {
	userBusyTimeout              uint32
	userLeaseTimeout             uint32
	concurrentTransactionLimitCh chan struct{}
	sendKeepAlive                bool
	backoffFactory               func() backoff.BackOff
}

func WithBusyTimeout(timeout int) func(c *connOptions) error {
	return func(c *connOptions) error {
		if timeout > 0 {
			c.userBusyTimeout = uint32(timeout)
		}
		return fmt.Errorf("%w: Timeout must be greater 0", Error)
	}
}

func WithLeaseTimeout(timeout int) func(c *connOptions) error {
	return func(c *connOptions) error {
		if timeout > 0 {
			c.userLeaseTimeout = uint32(timeout)
		}
		return fmt.Errorf("%w: Timeout must be greater 0", Error)
	}
}

// WithConcurrentTransactions limits the number of concurrent requests sent.
// If the option is not given in Dial, the concurrency is not limited.
func WithConcurrentTransactions(ct uint) func(c *connOptions) error {
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
func WithMeasureNetworkLatencyICMP() func(c *connOptions) error {
	// TODO
	return func(c *connOptions) error { return nil }
}
