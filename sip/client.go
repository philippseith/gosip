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

	Ping(options ...func(*requestOptions) error) <-chan error

	ReadEverything(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) <-chan Result[*ReadEverythingResponse]
	ReadOnlyData(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) <-chan Result[*ReadOnlyDataResponse]
	ReadDescription(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) <-chan Result[*ReadDescriptionResponse]
	ReadDataState(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) <-chan Result[*ReadDataStateResponse]

	WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte, options ...func(*requestOptions) error) <-chan error

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

// WithRetries configures how often a request is retried when it fails. Default is no retry.
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

func (c *client) Ping(options ...func(*requestOptions) error) <-chan error {
	return parseTryConnectDoWithErrChan(c, func() <-chan error {
		// Do not inline. c.Conn is not set yet
		return c.Conn.Ping()
	}, options...)
}

func (c *client) ReadEverything(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) <-chan Result[*ReadEverythingResponse] {
	return parseTryConnectDo[*ReadEverythingResponse](c, func() <-chan Result[*ReadEverythingResponse] {
		return c.Conn.ReadEverything(slaveIndex, slaveExtension, idn)
	}, options...)
}

func (c *client) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) <-chan Result[*ReadOnlyDataResponse] {
	return parseTryConnectDo[*ReadOnlyDataResponse](c, func() <-chan Result[*ReadOnlyDataResponse] {
		return c.Conn.ReadOnlyData(slaveIndex, slaveExtension, idn)
	}, options...)
}

func (c *client) ReadDescription(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) <-chan Result[*ReadDescriptionResponse] {
	return parseTryConnectDo[*ReadDescriptionResponse](c, func() <-chan Result[*ReadDescriptionResponse] {
		return c.Conn.ReadDescription(slaveIndex, slaveExtension, idn)
	}, options...)
}

func (c *client) ReadDataState(slaveIndex, slaveExtension int, idn uint32, options ...func(*requestOptions) error) <-chan Result[*ReadDataStateResponse] {
	return parseTryConnectDo[*ReadDataStateResponse](c, func() <-chan Result[*ReadDataStateResponse] {
		return c.Conn.ReadDataState(slaveIndex, slaveExtension, idn)
	}, options...)
}

func (c *client) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte, options ...func(*requestOptions) error) <-chan error {
	return parseTryConnectDoWithErrChan(c,
		func() <-chan error {
			return c.Conn.WriteData(slaveIndex, slaveExtension, idn, data)
		},
		options...)
}

func (c *client) Close() error {
	if c.Conn != nil {
		return c.Conn.Close()
	}
	return ErrorClosed
}

func parseTryConnectDoWithErrChan(c *client, do func() <-chan error, options ...func(*requestOptions) error) <-chan error {
	return mapChan[Result[struct{}], error](
		parseTryConnectDo[struct{}](c, func() <-chan Result[struct{}] {
			return mapChan[error, Result[struct{}]](
				do(),
				func(err error) Result[struct{}] {
					return Err[struct{}](err)
				}, 1)
		}, options...),
		func(r Result[struct{}]) error {
			return r.Err
		}, 1)
}

func parseTryConnectDo[T any](c *client, do func() <-chan Result[T], options ...func(*requestOptions) error) <-chan Result[T] {
	ch := make(chan Result[T], 1)
	go func() {
		defer close(ch)

		requestSettings, err := parseRequestOptions(options...)
		if err != nil {
			ch <- Err[T](err)
			return
		}

		errs := make([]error, 0, requestSettings.retries+1)
		for i := 0; i <= int(requestSettings.retries); i++ {

			err := c.tryConnect(requestSettings.ctx)
			if err != nil {
				errs = append(errs, err)
			} else {
				r := <-doWithCancel[T](requestSettings.ctx, do)
				if err == nil {
					ch <- r
					return
				}
				errs = append(errs, err)
			}
		}
		ch <- Err[T](errors.Join(errs...))
	}()
	return ch
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

func doWithCancel[T any](ctx context.Context, do func() <-chan Result[T]) <-chan Result[T] {
	ch := make(chan Result[T], 1)
	go func() {
		defer close(ch)

		select {
		case <-ctx.Done():
			ch <- Err[T](ctx.Err())
		case r := <-do():
			ch <- r
		}
	}()
	return ch
}
