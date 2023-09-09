package sip

import (
	"net"
	"time"
)

func Dial(network, address string, options ...func(c *Conn) error) (c *Conn, err error) {
	c = &Conn{timeoutReader: &timeoutReader{}}
	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}
	c.Conn, err = net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	c.timeoutReader.reader = c.Conn
	c.reqCh = make(chan request)
	c.respChans = map[uint32]chan func(PDU) (Exception, error){}
	go c.sendloop()
	go c.receiveLoop()
	return c, nil
}

func (c *Conn) Connect(busyTimeout, leaseTimeout int) (ex Exception, err error) {
	// TODO detect network latency and add it to the busy timeout
	// TODO Send Ping KeepAlive
	c.timeoutReader.SetTimeout(time.Duration(busyTimeout) * time.Millisecond)
	readResponse := c.sendWaitForResponse(&ConnectRequest{
		Version:      1,
		BusyTimeout:  uint32(busyTimeout),
		LeaseTimeout: uint32(leaseTimeout),
	})
	respPdu := &ConnectResponse{}
	if ex, err = readResponse(respPdu); ex.CommomErrorCode != 0 || err != nil {
		return ex, err
	}
	func() {
		c.mxCR.Lock()
		defer c.mxCR.Unlock()

		c.connectResponse = *respPdu
		c.timeoutReader.SetTimeout(time.Duration(c.connectResponse.BusyTimeout) * time.Millisecond)
	}()
	return ex, err
}

func (c *Conn) Connected() bool {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return c.connectResponse.Version != 0
}

func (c *Conn) BusyTimeout() int {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return int(c.connectResponse.BusyTimeout)
}

func (c *Conn) LeaseTimeout() int {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return int(c.connectResponse.LeaseTimeout)
}

func (c *Conn) MessageTypes() []uint32 {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return c.connectResponse.MessageTypes
}

func (c *Conn) Ping() (Exception, error) {
	return c.sendWaitForResponse(&PingRequest{})(&PingResponse{})
}

func (c *Conn) ReadEverything(slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, Exception, error) {
	resp := ReadEverythingResponse{}
	ex, err := c.sendWaitForResponse(&ReadEverythingRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
	return resp, ex, err
}

func (c *Conn) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, Exception, error) {
	resp := ReadOnlyDataResponse{}
	ex, err := c.sendWaitForResponse(&ReadOnlyDataRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
	return resp, ex, err
}

func (c *Conn) ReadDescription(slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, Exception, error) {
	resp := ReadDescriptionResponse{}
	ex, err := c.sendWaitForResponse(&ReadDescriptionRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
	return resp, ex, err
}

func (c *Conn) ReadDataState(slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, Exception, error) {
	resp := ReadDataStateResponse{}
	ex, err := c.sendWaitForResponse(&ReadDataStateRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
	return resp, ex, err
}

func (c *Conn) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte) (ex Exception, err error) {
	return c.sendWaitForResponse(&WriteDataRequest{
		writeDataRequest: writeDataRequest{
			SlaveIndex:     uint16(slaveIndex),
			SlaveExtension: uint16(slaveExtension),
			IDN:            idn,
			DataLength:     uint32(len(data)),
		},
		Data: data,
	})(&WriteDataResponse{})
}
