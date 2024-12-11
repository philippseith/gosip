package sip

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/joomcode/errorx"
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
	c, err := dial(context.Background(), network, address, options...)
	// See https://www.reddit.com/r/golang/comments/1bu5r72/subtle_and_surprising_behavior_when_interface/
	// A nil reference to conn is not the same as a nil Conn and can not compared to nil if returned als Conn
	if c == nil {
		return nil, err
	}
	return c, err
}

func dial(ctx context.Context, network, address string, options ...ConnOption) (*conn, error) {
	// Check for WithDial option
	wcOpts := &connOptions{}
	for _, option := range options {
		if err := option(wcOpts); err != nil {
			return nil, errorx.EnsureStackTrace(err)
		}
	}
	// Fallback to net.Dial
	if wcOpts.dial == nil {
		wcOpts.dial = func(network, address string) (io.ReadWriteCloser, error) {
			netConn, err := net.Dial(network, address)
			if err != nil {
				err = errorx.EnsureStackTrace(err)
			}
			return netConn, err
		}
	}
	netConn, err := wcOpts.dial(network, address)
	if err != nil {
		return nil, err
	}

	c := &conn{
		Conn: netConn,
		connOptions: connOptions{
			userBusyTimeout:  2000,
			userLeaseTimeout: 10000,
			mtu:              1450,
		},
		address:       address,
		timeoutReader: &timeoutReader{reader: netConn},

		reqCh:                make(chan request),
		transactionStartedCh: make(chan struct{}, 10000), // Practically infinite queue size, no memory allocation because of struct{} type
		respChans:            map[uint32]chan func(PDU) error{},
	}
	// Default: Allow practically infinite parallel transactions
	_ = WithConcurrentTransactionLimit(5000)(&c.connOptions)
	// But what does the user want?
	for _, option := range options {
		if err := option(&c.connOptions); err != nil {
			return nil, errorx.EnsureStackTrace(err)
		}
	}

	// Prepare corking
	if c.cork {
		c.mtuWriter, err = newCorkWriter(c.Conn, c.mtu, c.onFlush)
		if err != nil {
			logger.Printf("can not init corking: %v", err)
		}
	}
	// No corking, but make sure header and request are send in one datagram
	if c.mtuWriter == nil {
		c.mtuWriter = bufio.NewWriterSize(c.Conn, c.mtu)
	}

	// we use userBusy as BusyTimeout until the server responded
	c.connectResponse.BusyTimeout = c.userBusyTimeout

	sendRecvCtx, cancel := context.WithCancelCause(ctx)

	go c.sendLoop(sendRecvCtx, cancel)
	go c.receiveLoop(sendRecvCtx, cancel)
	go func() {
		<-sendRecvCtx.Done()
		c.cancelAllRequests(errorx.EnsureStackTrace(context.Cause(sendRecvCtx)))
		c.setClosed()
	}()

	return c, c.connect(ctx)
}

func (c *conn) Close() error {
	if c.cancel != nil {
		c.cancel(errorx.EnsureStackTrace(ErrorClosed))
	}
	c.setClosed()
	return c.cleanUp()
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
	return sendRequestWaitForResponseAndRead[*PingResponse](ctx, c, &PingRequest{}, &PingResponse{})
}

func (c *conn) ReadEverything(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, error) {
	req, resp := newReadEverythingPDUs(slaveIndex, slaveExtension, idn)
	err := sendRequestWaitForResponseAndRead[*ReadEverythingResponse](ctx, c, req, resp)
	return *resp, wrapErrorWithRequestInfo(err, slaveIndex, slaveExtension, idn)
}

func (c *conn) ReadOnlyData(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, error) {
	req, resp := newReadOnlyDataPDUs(slaveIndex, slaveExtension, idn)
	err := sendRequestWaitForResponseAndRead[*ReadOnlyDataResponse](ctx, c, req, resp)
	return *resp, wrapErrorWithRequestInfo(err, slaveIndex, slaveExtension, idn)
}

func (c *conn) ReadDescription(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, error) {
	req, resp := newReadDescriptionPDUs(slaveIndex, slaveExtension, idn)
	err := sendRequestWaitForResponseAndRead[*ReadDescriptionResponse](ctx, c, req, resp)
	return *resp, wrapErrorWithRequestInfo(err, slaveIndex, slaveExtension, idn)
}

func (c *conn) ReadDataState(ctx context.Context, slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, error) {
	req, resp := newReadDataStatePDUs(slaveIndex, slaveExtension, idn)
	err := sendRequestWaitForResponseAndRead[*ReadDataStateResponse](ctx, c, req, resp)
	return *resp, wrapErrorWithRequestInfo(err, slaveIndex, slaveExtension, idn)
}

func (c *conn) WriteData(ctx context.Context, slaveIndex, slaveExtension int, idn uint32, data []byte) error {
	if slaveIndex < 0 || slaveIndex > 0xFFFF {
		return errorx.EnsureStackTrace(fmt.Errorf("slaveIndex out of range [0-65535]: %v", slaveIndex))
	}
	u16slaveIndex := uint16(slaveIndex)
	if slaveExtension < 0 || slaveExtension > 0xFFFF {
		return errorx.EnsureStackTrace(fmt.Errorf("slaveExtension out of range [0-65535]: %v", slaveIndex))
	}
	u16slaveExtension := uint16(slaveExtension)
	if len(data) > 0xFFFF {
		return errorx.EnsureStackTrace(fmt.Errorf("data length out of range [0-65535]: %v", len(data)))
	}
	err := sendRequestWaitForResponseAndRead[*WriteDataResponse](ctx, c, &WriteDataRequest{
		writeDataRequest: writeDataRequest{
			Request: Request{
				SlaveIndex:     u16slaveIndex,
				SlaveExtension: u16slaveExtension,
				IDN:            idn,
			},
			DataLength: uint32(len(data)), //nolint:gosec
		},
		Data: data,
	}, &WriteDataResponse{})
	return wrapErrorWithRequestInfo(err, slaveIndex, slaveExtension, idn)
}

func wrapErrorWithRequestInfo(err error, slaveIndex, slaveExtension int, idn uint32) error {
	if err != nil {
		return fmt.Errorf("%w %v %v %v", err, slaveIndex, slaveExtension, Idn(idn))
	}
	return err
}
