package sip

import (
	"context"
	"time"

	"github.com/joomcode/errorx"
)

// RequestOption is a option for any kind of request
type RequestOption func(r RequestOptions) error

type RequestOptions interface {
	asRequestOptions() *requestOptions
	SetMetaData(key string, value any)
	GetMetaData(key string) (any, bool)
}

// WithRetries configures how often a request is retried when it fails. Default is no retry.
func WithRetries(retries uint) RequestOption {
	return func(r RequestOptions) error {
		r.asRequestOptions().retries = retries
		return nil
	}
}

// WithContext allows to cancel a request with a context.
func WithContext(ctx context.Context) RequestOption {
	return func(r RequestOptions) error {
		r.asRequestOptions().ctx = ctx
		return nil
	}
}

// WithTimeout allows to cancel a request with a timeout.
func WithTimeout(timeout time.Duration) RequestOption {
	return func(r RequestOptions) error {
		r.asRequestOptions().timeout = timeout
		return nil
	}
}

type requestOptions struct {
	retries  uint
	ctx      context.Context
	timeout  time.Duration
	metaData map[string]any
}

func (r *requestOptions) asRequestOptions() *requestOptions {
	return r
}

func (r *requestOptions) SetMetaData(key string, value any) {
	if r.metaData == nil {
		r.metaData = make(map[string]any)
	}
	r.metaData[key] = value
}

func (r *requestOptions) GetMetaData(key string) (any, bool) {
	if r.metaData == nil {
		return nil, false
	}
	value, ok := r.metaData[key]
	return value, ok
}

func ParseRequestOptions(options ...RequestOption) (RequestOptions, context.CancelFunc, error) {
	r := &requestOptions{
		ctx: context.Background(),
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(r); err != nil {
			return r, func() {}, errorx.EnsureStackTrace(err)
		}
	}
	if r.timeout > 0 {
		var cancel context.CancelFunc
		r.ctx, cancel = context.WithTimeout(r.ctx, r.timeout)
		return r, cancel, nil
	}
	return r, func() {}, nil
}
