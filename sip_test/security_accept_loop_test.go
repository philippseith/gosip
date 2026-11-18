package sip_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/require"
)

type failingListener struct {
	accepts atomic.Int64
	err     error
	closed  chan struct{}
	once    sync.Once
}

func (l *failingListener) Accept() (net.Conn, error) {
	l.accepts.Add(1)
	return nil, l.err
}
func (l *failingListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *failingListener) Addr() net.Addr { return nil }

// Accept errors (EMFILE under connection pressure, or a listener the caller has
// closed without cancelling the context) must not turn the accept loop into a
// busy loop that also floods stderr.
func TestServeAcceptErrorsDoNotSpin(t *testing.T) {
	for name, acceptErr := range map[string]error{
		"listener closed by caller": net.ErrClosed,
		"transient (EMFILE-like)":   errors.New("accept tcp: too many open files"),
	} {
		t.Run(name, func(t *testing.T) {
			listener := &failingListener{err: acceptErr, closed: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, sip.Serve(ctx, listener, pingSource{}))
			time.Sleep(300 * time.Millisecond)
			calls := listener.accepts.Load()
			t.Logf("%d Accept calls in 300 ms", calls)
			require.Less(t, calls, int64(1000), "accept loop spins on errors")
		})
	}
}
