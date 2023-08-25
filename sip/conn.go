package sip

import "net"

type Conn struct {
	net.Conn
	response ConnectResponse
}

func (c *Conn) Connect(network, address string, busyTimeout, leaseTimeout int) (err error) {
	c.Conn, err = net.Dial(network, address)
	if err != nil {
		return err
	}
	c.response, err = Connect(c.Conn, busyTimeout, leaseTimeout)
	if err != nil {
		return err
	}
	return nil
}

func (c *Conn) Connected() bool {
	return c.response.Version != 0
}

func (c *Conn) BusyTimeout() int {
	return int(c.response.BusyTimeout)
}

func (c *Conn) LeaseTimeout() int {
	return int(c.response.LeaseTimeout)
}

func (c *Conn) SupportedMessageTypes() []uint32 {
	return c.response.MessageTypes
}

func (c *Conn) Ping() error {
	return Ping(c.Conn)
}

func (c *Conn) ReadEverything(slaveIndex, slaveExtension int, idn uint32) (response ReadEverythingResponse, err error) {
	return ReadEverything(c, slaveIndex, slaveExtension, idn)
}
