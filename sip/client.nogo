package sip

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// Client is like Conn, but with automatic reconnect, a configurable timeout
// strategy for reconnection, the possibility to cancel single requests with a
// context.Context and to automatically retry requests.
//
// Ping, ReadXXX, WriteData work like the Conn methods but try to reconnect when
// the underlying connection has been closed meanwhile. Their further behavior
// can be configured with options.
//
// Close closes the currently open connection, if there is any yet.
type Client interface {
	ConnProperties

	Ping(options ...func(*requestOptions) error) error

	ReadEverything(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) (ReadEverythingResponse, error)
	ReadOnlyData(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) (ReadOnlyDataResponse, error)
	ReadDescription(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) (ReadDescriptionResponse, error)
	ReadDataState(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) (ReadDataStateResponse, error)

	WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte, options ...func(*requestOptions) error) error

	Close() error
}

// NewClient creates a new Client. The backoff strategy for failed connects can
// be configured by the WithDialBackoff option. If the option is not given, the
// backoff strategy is an exponential backoff, starting with a backoff time of
// 500ms, exponentially incremented by factor 1.5, with an overall timeout of 10
// seconds. All other options are ignored.
func NewClient(network, address string, options ...func(c *connOptions) error) Client {
	co := &connOptions{
		backoffFactory: func() backoff.BackOff {
			b := backoff.NewExponentialBackOff()
			b.MaxElapsedTime = 10 * time.Second
			return b
		},
	}
	for _, option := range options {
		_ = option(co)
	}
	return &client{
		network:        network,
		address:        address,
		options:        options,
		backoffFactory: co.backoffFactory,
	}
}

// WithDialBackoff configures the backoff strategy for failed connects. See
// https://pkg.go.dev/github.com/cenkalti/backoff/v4 for more information about
// backoff strategies. Default is an exponential backoff, starting with a
// backoff time of 500ms, exponentially incremented by factor 1.5, with an
// overall timeout of 10 seconds.
func WithDialBackoff(backoffFactory func() backoff.BackOff) func(c *connOptions) error {
	return func(c *connOptions) error {
		c.backoffFactory = backoffFactory
		return nil
	}
}

// WithRetries configures how often a request is retried if it fails. Default is no retry.
func WithRetries(retries uint) func(r *requestOptions) error {
	return func(r *requestOptions) error {
		r.retries = retries
		return nil
	}
}

// WithContext allows to cancel a request with a context.
func WithContext(ctx context.Context) func(r *requestOptions) error {
	return func(r *requestOptions) error {
		r.ctx = ctx
		return nil
	}
}

type requestOptions struct {
	retries uint
	ctx     context.Context
}

func parseRequestOptions(options ...func(*requestOptions) error) (*requestOptions, error) {
	r := &requestOptions{
		ctx: context.Background(),
	}
	for _, option := range options {
		if err := option(r); err != nil {
			return r, err
		}
	}
	return r, nil
}

type client struct {
	Conn
	network        string
	address        string
	options        []func(c *connOptions) error
	backoffFactory func() backoff.BackOff
	backoff        backoff.BackOff
}

func (c *client) BusyTimeout() time.Duration {
	if c.Conn == nil {
		return time.Millisecond * time.Duration(0)
	}
	return c.Conn.BusyTimeout()
}
func (c *client) LeaseTimeout() time.Duration {
	if c.Conn == nil {
		return time.Millisecond * time.Duration(0)
	}
	return c.Conn.LeaseTimeout()
}

func (c *client) LastReceived() time.Time {
	if c.Conn == nil {
		return time.Time{}
	}
	return c.Conn.LastReceived()
}

func (c *client) MessageTypes() []uint32 {
	if c.Conn == nil {
		return nil
	}
	return c.Conn.MessageTypes()
}

func (c *client) Ping(options ...func(*requestOptions) error) error {
	_, err := parseTryConnectDo[struct{}](c, func() (struct{}, error) {
		return struct{}{}, c.Conn.Ping()
	}, options...)
	return err
}

func (c *client) ReadEverything(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) (ReadEverythingResponse, error) {
	return parseTryConnectDo[ReadEverythingResponse](c, func() (ReadEverythingResponse, error) {
		return c.Conn.ReadEverything(slaveIndex, slaveExtension, idn)
	}, options...)
}

func (c *client) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) (ReadOnlyDataResponse, error) {
	return parseTryConnectDo[ReadOnlyDataResponse](c, func() (ReadOnlyDataResponse, error) {
		return c.Conn.ReadOnlyData(slaveIndex, slaveExtension, idn)
	}, options...)
}

func (c *client) ReadDescription(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) (ReadDescriptionResponse, error) {
	return parseTryConnectDo[ReadDescriptionResponse](c, func() (ReadDescriptionResponse, error) {
		return c.Conn.ReadDescription(slaveIndex, slaveExtension, idn)
	}, options...)
}

func (c *client) ReadDataState(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) (ReadDataStateResponse, error) {
	return parseTryConnectDo[ReadDataStateResponse](c, func() (ReadDataStateResponse, error) {
		return c.Conn.ReadDataState(slaveIndex, slaveExtension, idn)
	}, options...)
}

func (c *client) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte, options ...func(*requestOptions) error) error {
	_, err := parseTryConnectDo[struct{}](c, func() (struct{}, error) {
		return struct{}{}, c.Conn.WriteData(slaveIndex, slaveExtension, idn, data)
	}, options...)
	return err
}

func (c *client) Close() error {
	if c.Conn != nil {
		return c.Conn.Close()
	}
	return ErrorClosed
}

func parseTryConnectDo[T any](c *client, do func() (T, error), options ...func(*requestOptions) error) (T, error) {
	o, err := parseRequestOptions(options...)
	if err != nil {
		return *new(T), err
	}

	errs := make([]error, 0, o.retries+1)
	for i := 0; i <= int(o.retries); i++ {

		err := c.tryConnect(o.ctx)
		if err != nil {
			errs = append(errs, err)
		} else {
			t, err := doWithCancel[T](o.ctx, do)
			if err == nil {
				return t, nil
			}
			errs = append(errs, err)
		}
	}
	return *new(T), errors.Join(errs...)
}

func (c *client) tryConnect(ctx context.Context) (err error) {
	if c.Conn != nil &&
		c.Conn.Connected() &&
		time.Since(c.Conn.LastReceived()) < c.Conn.LeaseTimeout() {
		return nil
	}
	if c.backoff == nil {
		c.backoff = c.backoffFactory()
	}
	var spanSum time.Duration
	// Try to connect until the
	for {
		// ctx is for canceling the request, not the whole connection, so we silence contextchecks complains.
		// nolint:contextcheck
		c.Conn, err = Dial(c.network, c.address, c.options...)
		if err == nil {
			// When connecting worked, the backoff has to start anew with the next request
			c.backoff = nil
			return nil
		}
		backoffSpan := c.backoff.NextBackOff()
		if backoffSpan == backoff.Stop {
			return errors.Join(fmt.Errorf("%w: %v", ErrorRetriesExceeded, spanSum), err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoffSpan):
			spanSum += backoffSpan
		}
	}
}

func doWithCancel[T any](ctx context.Context, do func() (T, error)) (T, error) {
	ch := make(chan Result[T])
	defer close(ch)

	go func() {
		r := Result[T]{}
		r.Ok, r.Err = do()
		ch <- r
	}()
	select {
	case <-ctx.Done():
		return *new(T), ctx.Err()
	case r := <-ch:
		return r.Ok, r.Err
	}
}

// func goWithCancel[T any](ctx context.Context, f func() (T, error)) <-chan Result[T] {
// 	ch := make(chan Result[T], 1)
// 	go func() {
// 		defer close(ch)

// 		select {
// 		case <-ctx.Done():
// 			ch <- Err[T](ctx.Err())
// 		case r := <-func() <-chan Result[T] {
// 			chf := make(chan Result[T])
// 			go func() {
// 				r := Result[T]{}
// 				r.Ok, r.Err = f()
// 				chf <- r
// 				close(chf)
// 			}()
// 			return chf
// 		}():
// 			ch <- r
// 		}
// 	}()
// 	return ch
// }
