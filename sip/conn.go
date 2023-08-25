package sip

import "net"

type Conn struct {
	net.Conn
	response ConnectResponse
}

func (c *Conn) Connect(network, address string, busyTimeout, leaseTimeout int) (ex Exception, err error) {
	c.Conn, err = net.Dial(network, address)
	if err != nil {
		return ex, err
	}
	c.response, ex, err = Connect(c.Conn, busyTimeout, leaseTimeout)
	return ex, err
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

func (c *Conn) Ping() (Exception, error) {
	return Ping(c.Conn)
}

func (c *Conn) ReadEverything(slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, Exception, error) {
	return ReadEverything(c, slaveIndex, slaveExtension, idn)
}

func (c *Conn) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, Exception, error) {
	return ReadOnlyData(c, slaveIndex, slaveExtension, idn)
}

func (c *Conn) ReadDescription(slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, Exception, error) {
	return ReadDescription(c, slaveIndex, slaveExtension, idn)
}

func (c *Conn) ReadDataState(slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, Exception, error) {
	return ReadDataState(c, slaveIndex, slaveExtension, idn)
}

func (c *Conn) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte) (ex Exception, err error) {
	return WriteData(c, slaveIndex, slaveExtension, idn, data)
}
