package sip

import (
	"context"
	"net"
	"time"
)

// Conn is a SIP client connection. It can be used to read and write SERCOS
// parameter values. Its lifetime starts with Dial() and ends when the user
// calls Close(), the server does not answer in timely manner (BusyTimeout) or
// the underlying net.Conn has been closed.
type Conn interface {
	ConnProperties

	Ping() <-chan error

	ReadEverything(slaveIndex, slaveExtension int, idn uint32) <-chan Result[*ReadEverythingResponse]
	ReadOnlyData(slaveIndex, slaveExtension int, idn uint32) <-chan Result[*ReadOnlyDataResponse]
	ReadDescription(slaveIndex, slaveExtension int, idn uint32) <-chan Result[*ReadDescriptionResponse]
	ReadDataState(slaveIndex, slaveExtension int, idn uint32) <-chan Result[*ReadDataStateResponse]
	WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte) <-chan error

	Close() error
}

// ConnProperties describes the properties of a Conn.
//
//	Connected() bool
//
// If the Conn is currently connected
//
//	BusyTimeout() time.Duration
//
// Time span after the server acknowledged to send a Busy response if it is not
// able to respond to open requests.
//
//	LeaseTimeout() time.Duration
//
// Time span without requests after the server will close the connection. When
// the WithKeepAlive() option is used, the connection will send a Ping request
// shortly before this time span ends.
//
//	LastReceived() time.Time
//
// Timestamp when the last response from the server was received. The Client
// uses this information to decide if to send a request over the current
// connection or to open a new one. By this, timeouts caused by servers which do
// not close the net.Conn directly after they decide to close the SIP connection
// can be evaded.
type ConnProperties interface {
	Connected() bool

	BusyTimeout() time.Duration
	LeaseTimeout() time.Duration
	LastReceived() time.Time

	MessageTypes() []uint32
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
			userBusyTimeout:  2000,
			userLeaseTimeout: 10000,
		},
		timeoutReader: &timeoutReader{reader: netConn},

		reqCh:                make(chan request),
		transactionStartedCh: make(chan struct{}, 5000), // Practically infinite queue size, no memory allocation because of struct{} type
		respChans:            map[uint32]chan func(PDU) error{},
	}
	// Default: Allow practically infinite parallel transactions
	_ = WithConcurrentTransactions(5000)(&c.connOptions)
	// But what does the user want?
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

func (c *conn) Ping() <-chan error {
	ch := make(chan error, 1)
	go func() {
		respFunc := <-c.sendAndWaitForResponse(&PingRequest{})
		ch <- respFunc((&PingResponse{}))
		close(ch)
	}()
	return ch
}

func (c *conn) ReadEverything(slaveIndex, slaveExtension int, idn uint32) <-chan Result[*ReadEverythingResponse] {
	ch := make(chan Result[*ReadEverythingResponse], 1)
	go readResponse[*ReadEverythingRequest, *ReadEverythingResponse](c, slaveIndex, slaveExtension, idn, ch)
	return ch
}

func (c *conn) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32) <-chan Result[*ReadOnlyDataResponse] {
	ch := make(chan Result[*ReadOnlyDataResponse], 1)
	go readResponse[*ReadOnlyDataRequest, *ReadOnlyDataResponse](c, slaveIndex, slaveExtension, idn, ch)
	return ch
}

func (c *conn) ReadDescription(slaveIndex, slaveExtension int, idn uint32) <-chan Result[*ReadDescriptionResponse] {
	ch := make(chan Result[*ReadDescriptionResponse], 1)
	go readResponse[*ReadDescriptionRequest, *ReadDescriptionResponse](c, slaveIndex, slaveExtension, idn, ch)
	return ch
}

func (c *conn) ReadDataState(slaveIndex, slaveExtension int, idn uint32) <-chan Result[*ReadDataStateResponse] {
	ch := make(chan Result[*ReadDataStateResponse], 1)
	go readResponse[*ReadDataStateRequest, *ReadDataStateResponse](c, slaveIndex, slaveExtension, idn, ch)
	return ch
}

func (c *conn) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte) <-chan error {
	ch := make(chan error, 1)
	go func() {
		respFunc := <-c.sendAndWaitForResponse(&WriteDataRequest{
			writeDataRequest: writeDataRequest{
				SlaveIndex:     uint16(slaveIndex),
				SlaveExtension: uint16(slaveExtension),
				IDN:            idn,
				DataLength:     uint32(len(data)),
			},
			Data: data,
		})
		ch <- respFunc((&WriteDataResponse{}))
		close(ch)
	}()
	return ch
}
