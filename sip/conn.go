package sip

import (
	"time"
)

// Dial opens a sip.Conn.
func Dial(network, address string, options ...func(c *Conn) error) (c *Conn, err error) {
	c = &Conn{
		timeoutReader:            &timeoutReader{},
		userBusyTimeout:          2000,
		userLeaseTimeout:         10000,
		concurrentTransactionsCh: make(chan struct{}, 4000), // Practically infinite queue size, no memory allocation because of struct{} type
	}
	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}
	// we use userBusy as BusyTimeout until the server responded
	c.connectResponse.BusyTimeout = c.userBusyTimeout

	err = <-c.connLoop(network, address)
	return c, err
}

func (c *Conn) Close() error {
	if c.cancel != nil {
		c.cancel(ErrorClosed)
	}
	return c.cleanUp()
}

func (c *Conn) cleanUp() (err error) {
	c.mxState.Lock()
	defer c.mxState.Unlock()

	if c.reqCh != nil {
		close(c.reqCh)
		c.reqCh = nil
	}
	if c.concurrentTransactionsCh != nil {
		close(c.concurrentTransactionsCh)
		c.concurrentTransactionsCh = nil
	}
	if c.concurrentTransactionLimitCh != nil {
		close(c.concurrentTransactionLimitCh)
		c.concurrentTransactionLimitCh = nil
	}

	if c.Conn != nil {
		err = c.Conn.Close()
		c.Conn = nil
	}
	return err
}

func (c *Conn) Connected() bool {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return c.connectResponse.Version != 0
}

func (c *Conn) BusyTimeout() time.Duration {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return time.Millisecond * time.Duration(c.connectResponse.BusyTimeout)
}

func (c *Conn) LeaseTimeout() time.Duration {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return time.Millisecond * time.Duration(c.connectResponse.LeaseTimeout)
}

func (c *Conn) MessageTypes() []uint32 {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return c.connectResponse.MessageTypes
}

func (c *Conn) Ping() error {
	return c.sendWaitForResponse(&PingRequest{})(&PingResponse{})
}

func (c *Conn) ReadEverything(slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, error) {
	resp := ReadEverythingResponse{}
	return resp, c.sendWaitForResponse(&ReadEverythingRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
}

func (c *Conn) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, error) {
	resp := ReadOnlyDataResponse{}
	return resp, c.sendWaitForResponse(&ReadOnlyDataRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
}

func (c *Conn) ReadDescription(slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, error) {
	resp := ReadDescriptionResponse{}
	return resp, c.sendWaitForResponse(&ReadDescriptionRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
}

func (c *Conn) ReadDataState(slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, error) {
	resp := ReadDataStateResponse{}
	return resp, c.sendWaitForResponse(&ReadDataStateRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
}

func (c *Conn) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte) error {
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
