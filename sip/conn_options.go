package sip

import "fmt"

func BusyTimeout(timeout int) func(c *Conn) error {
	return func(c *Conn) error {
		if timeout > 0 {
			c.userBusyTimeout = uint32(timeout)
		}
		return fmt.Errorf("%w: Timeout must be greater 0", Error)
	}
}

func LeaseTimeout(timeout int) func(c *Conn) error {
	return func(c *Conn) error {
		if timeout > 0 {
			c.userLeaseTimeout = uint32(timeout)
		}
		return fmt.Errorf("%w: Timeout must be greater 0", Error)
	}
}

// ConcurrentTransactions limits the number of concurrent requests sent.
// If the option is not given in Dial, the concurrency is not limited.
func ConcurrentTransactions(ct uint) func(c *Conn) error {
	return func(c *Conn) error {
		if ct > 0 {
			c.concurrentTransactionLimitCh = make(chan struct{}, ct)
		}
		return nil
	}
}

// SendKeepAlive configures the connection that it is sending Ping requests
// shortly before the LeaseTimeout ends.
func SendKeepAlive() func(c *Conn) error {
	return func(c *Conn) error {
		c.sendKeepAlive = true
		return nil
	}
}

// MeasureNetworkLatencyICMP measures the network latency with an ICMP ping.
// If not set, the network latency is measured with S/IP Ping, which might lead
// to different latency results, depending on the server implementation.
// Note that ICMP ping requires specific system config options mentioned here:
// https://github.com/prometheus-community/#supported-operating-systems
func MeasureNetworkLatencyICMP() func(c *Conn) error {
	// TODO
	return func(c *Conn) error { return nil }
}
