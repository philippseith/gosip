package sip

import (
	"context"
	"io"
	"net"
	"time"

	"braces.dev/errtrace"
)

// Conn is a SIP client connection. It can be used to read and write SERCOS
// parameter values. Its lifetime starts with Dial and ends when the user
// calls Close, the server does not answer in timely manner (BusyTimeout) or
// the underlying net.Conn has been closed.
//
// When Dial is successful, the S/IP Connect procedure has already been executed.
// Ping, ReadXXX, WriteData send the according Request and try to receive the matching Response.
// They return either with an error or when the Response has been received successfuly.
//
// Conn is able to execute more than one concurrent transaction, see WithConcurrentTransactionLimit.
type Conn interface {
	ConnProperties

	Ping(ctx context.Context) error

	ReadEverything(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, error)
	ReadOnlyData(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, error)
	ReadDescription(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, error)
	ReadDataState(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, error)
	WriteData(ctx context.Context, slaveIndex, slaveExtension int, idn uint32, data []byte) error

	Close() error
	Closed() bool
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

// Dial opens a Conn and connects it.
func Dial(network, address string, options ...ConnOption) (Conn, error) {
	return dial(context.Background(), network, address, options...)
}

func dial(ctx context.Context, network, address string, options ...ConnOption) (*conn, error) {
	// Check for WithConnnection option
	wcOpts := &connOptions{}
	for _, option := range options {
		if err := option(wcOpts); err != nil {
			return nil, errtrace.Wrap(err)
		}
	}
	// Fallback to net.Dial
	if wcOpts.dial == nil {
		wcOpts.dial = func(network, address string) (io.ReadWriteCloser, error) {
			return net.Dial(network, address)
		}
	}
	netConn, err := wcOpts.dial(network, address)
	if err != nil {
		return nil, errtrace.Wrap(err)
	}

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
	_ = WithConcurrentTransactionLimit(5000)(&c.connOptions)
	// But what does the user want?
	for _, option := range options {
		if err := option(&c.connOptions); err != nil {
			return nil, errtrace.Wrap(err)
		}
	}
	// we use userBusy as BusyTimeout until the server responded
	c.connectResponse.BusyTimeout = c.userBusyTimeout

	sendRecvCtx, cancel := context.WithCancelCause(ctx)

	go c.sendLoop(sendRecvCtx, cancel)
	go c.receiveLoop(sendRecvCtx, cancel)
	go func() {
		<-sendRecvCtx.Done()
		c.cancelAllRequests(errtrace.Wrap(context.Cause(sendRecvCtx)))
		c.setClosed()
	}()

	return c, errtrace.Wrap(c.connect(ctx))
}

func (c *conn) Close() error {
	if c.cancel != nil {
		c.cancel(ErrorClosed)
	}
	c.setClosed()
	return errtrace.Wrap(c.cleanUp())
}

func (c *conn) setClosed() {
	c.mxState.Lock()
	defer c.mxState.Unlock()

	c.closed = true
}

func (c *conn) Closed() bool {
	c.mxState.RLock()
	defer c.mxState.RUnlock()

	return c.closed
}

func (c *conn) Connected() bool {
	c.mxCR.RLock()
	c.mxState.RLock()

	defer c.mxCR.RUnlock()
	defer c.mxState.RUnlock()

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

func (c *conn) Ping(ctx context.Context) error {
	return errtrace.Wrap(sendRequestWaitForResponseAndRead[*PingResponse](ctx, c, &PingRequest{}, &PingResponse{}))
}

func (c *conn) ReadEverything(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, error) {
	req, resp := newReadEverythingPDUs(slaveIndex, slaveExtension, idn)
	err := sendRequestWaitForResponseAndRead[*ReadEverythingResponse](ctx, c, req, resp)
	return *resp, errtrace.Wrap(err)
}

func (c *conn) ReadOnlyData(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, error) {
	req, resp := newReadOnlyDataPDUs(slaveIndex, slaveExtension, idn)
	err := sendRequestWaitForResponseAndRead[*ReadOnlyDataResponse](ctx, c, req, resp)
	return *resp, errtrace.Wrap(err)
}

func (c *conn) ReadDescription(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, error) {
	req, resp := newReadDescriptionPDUs(slaveIndex, slaveExtension, idn)
	err := sendRequestWaitForResponseAndRead[*ReadDescriptionResponse](ctx, c, req, resp)
	return *resp, errtrace.Wrap(err)
}

func (c *conn) ReadDataState(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, error) {
	req, resp := newReadDataStatePDUs(slaveIndex, slaveExtension, idn)
	err := sendRequestWaitForResponseAndRead[*ReadDataStateResponse](ctx, c, req, resp)
	return *resp, errtrace.Wrap(err)
}

func (c *conn) WriteData(ctx context.Context, slaveIndex, slaveExtension int, idn uint32, data []byte) error {
	return errtrace.Wrap(sendRequestWaitForResponseAndRead[*WriteDataResponse](ctx, c, &WriteDataRequest{
		writeDataRequest: writeDataRequest{
			Request: Request{
				SlaveIndex:     uint16(slaveIndex),
				SlaveExtension: uint16(slaveExtension),
				IDN:            idn,
			},
			DataLength: uint32(len(data)),
		},
		Data: data,
	}, &WriteDataResponse{}))
}
