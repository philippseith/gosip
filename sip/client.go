package sip

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// Client is like Conn, but
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

func NewClient(network, address string, options ...func(c *connOptions) error) Client {
	co := &connOptions{
		backoffFactory: func() backoff.BackOff { return backoff.NewExponentialBackOff() },
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

func WithBackoff(backoffFactory func() backoff.BackOff) func(c *connOptions) error {
	return func(c *connOptions) error {
		c.backoffFactory = backoffFactory
		return nil
	}
}

func WithRetries(retries uint) func(r *requestOptions) error {
	return func(r *requestOptions) error {
		r.retries = retries
		return nil
	}
}

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
	} else {
		boff := c.backoff.NextBackOff()
		if boff == backoff.Stop {
			return ErrorRetriesExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(boff):
		}
	}
	// ctx is for canceling the request, not the whole connection
	// nolint:contextcheck
	c.Conn, err = Dial(c.network, c.address, c.options...)
	return err
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
