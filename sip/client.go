package sip

import (
	"time"

	"github.com/cenkalti/backoff/v4"
)

// Client is like Conn, but
type Client interface {
	Conn
}

func NewClient(network, address string, options ...func(c *connOptions) error) Client {
	return &client{
		network: network,
		address: address,
		options: options,
	}
}

func WithBackoff(backoffFactory func() backoff.BackOff) func(c *connOptions) error {
	return func(c *connOptions) error {
		c.backoffFactory = backoffFactory
		return nil
	}
}

type client struct {
	Conn
	network string
	address string
	options []func(c *connOptions) error
}

func (c *client) Ping() error {
	if err := c.tryConnect(); err != nil {
		return err
	}
	return c.Conn.Ping()
}

func (c *client) ReadEverything(slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, error) {
	if err := c.tryConnect(); err != nil {
		return ReadEverythingResponse{}, err
	}
	return c.Conn.ReadEverything(slaveIndex, slaveExtension, idn)
}

func (c *client) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, error) {
	if err := c.tryConnect(); err != nil {
		return ReadOnlyDataResponse{}, err
	}
	return c.Conn.ReadOnlyData(slaveIndex, slaveExtension, idn)
}

func (c *client) ReadDescription(slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, error) {
	if err := c.tryConnect(); err != nil {
		return ReadDescriptionResponse{}, err
	}
	return c.Conn.ReadDescription(slaveIndex, slaveExtension, idn)
}

func (c *client) ReadDataState(slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, error) {
	if err := c.tryConnect(); err != nil {
		return ReadDataStateResponse{}, err
	}
	return c.Conn.ReadDataState(slaveIndex, slaveExtension, idn)
}

func (c *client) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte) error {
	if err := c.tryConnect(); err != nil {
		return err
	}
	return c.Conn.WriteData(slaveIndex, slaveExtension, idn, data)
}

func (c *client) tryConnect() (err error) {
	if c.Conn != nil &&
		c.Conn.Connected() &&
		time.Since(c.Conn.LastReceived()) < c.Conn.LeaseTimeout() {
		return nil
	}
	// TODO Backoff
	c.Conn, err = Dial(c.network, c.address, c.options...)
	return err
}
