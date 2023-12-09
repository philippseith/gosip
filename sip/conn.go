package sip

import (
	"context"
	"net"
	"time"
)

// Conn is a SIP client connection. It can be used to read and write SERCOS parameter values.
// Its lifetime starts witth Dial() and ends when the user calls Close(),
// the server does not anwser in timely manner (BusyTimeout) or the underlying net.Conn has been closed.
type Conn interface {
	Connected() bool

	BusyTimeout() time.Duration
	LeaseTimeout() time.Duration
	LastReceived() time.Time

	MessageTypes() []uint32

	Ping() error

	ReadEverything(slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, error)
	ReadOnlyData(slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, error)
	ReadDescription(slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, error)
	ReadDataState(slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, error)
	WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte) error

	Close() error
}

// Dial opens a sip.Conn.
func Dial(network, address string, options ...func(c *connOptions) error) (Conn, error) {
	netConn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	sendRecvCtx, cancel := context.WithCancelCause(context.Background())

	c := &conn{
		Conn: netConn,
		connOptions: connOptions{
			userBusyTimeout:              2000,
			userLeaseTimeout:             10000,
			concurrentTransactionLimitCh: make(chan struct{}, 5000), // Practically infinite queue size
		},
		timeoutReader: &timeoutReader{reader: netConn},

		reqCh:                make(chan request),
		transactionStartedCh: make(chan struct{}, 5000), // Practically infinite queue size, no memory allocation because of struct{} type
		respChans:            map[uint32]chan func(PDU) error{},
	}
	for _, option := range options {
		if err := option(&c.connOptions); err != nil {
			return nil, err
		}
	}
	// we use userBusy as BusyTimeout until the server responded
	c.connectResponse.BusyTimeout = c.userBusyTimeout

	go c.sendLoop(sendRecvCtx, cancel)
	go c.receiveLoop(sendRecvCtx, cancel)
	go func() {
		<-sendRecvCtx.Done()
		c.cancelAllRequests(context.Cause(sendRecvCtx))
	}()

	return c, c.connect()
}

func (c *conn) Close() error {
	if c.cancel != nil {
		c.cancel(ErrorClosed)
	}
	return c.cleanUp()
}

func (c *conn) Connected() bool {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return c.Conn != nil && c.connectResponse.Version != 0
}

func (c *conn) BusyTimeout() time.Duration {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return time.Millisecond * time.Duration(c.connectResponse.BusyTimeout)
}

func (c *conn) LeaseTimeout() time.Duration {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return time.Millisecond * time.Duration(c.connectResponse.LeaseTimeout)
}

func (c *conn) LastReceived() time.Time {
	c.mxState.RLock()
	defer c.mxState.RUnlock()

	return c.lastReceived
}

func (c *conn) MessageTypes() []uint32 {
	c.mxCR.RLock()
	defer c.mxCR.RUnlock()

	return c.connectResponse.MessageTypes
}

func (c *conn) Ping() error {
	return c.sendAndWaitForResponse(&PingRequest{})(&PingResponse{})
}

func (c *conn) ReadEverything(slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, error) {
	resp := ReadEverythingResponse{}
	return resp, c.sendAndWaitForResponse(&ReadEverythingRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
}

func (c *conn) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, error) {
	resp := ReadOnlyDataResponse{}
	return resp, c.sendAndWaitForResponse(&ReadOnlyDataRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
}

func (c *conn) ReadDescription(slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, error) {
	resp := ReadDescriptionResponse{}
	return resp, c.sendAndWaitForResponse(&ReadDescriptionRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
}

func (c *conn) ReadDataState(slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, error) {
	resp := ReadDataStateResponse{}
	return resp, c.sendAndWaitForResponse(&ReadDataStateRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	})(&resp)
}

func (c *conn) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte) error {
	return c.sendAndWaitForResponse(&WriteDataRequest{
		writeDataRequest: writeDataRequest{
			SlaveIndex:     uint16(slaveIndex),
			SlaveExtension: uint16(slaveExtension),
			IDN:            idn,
			DataLength:     uint32(len(data)),
		},
		Data: data,
	})(&WriteDataResponse{})
}
