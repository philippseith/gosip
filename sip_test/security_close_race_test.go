package sip_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/require"
)

// Closing a Conn while its receive loop is still handing a transaction slot
// back must neither race nor panic. Before the fix, cleanUp closed the
// transaction channels while signal could still send on them ("send on closed
// channel" in the receive loop goroutine, which ends the whole process). The race
// is reported by `go test -race`; without the race detector the window is too
// narrow to hit in a normal run.
func TestCloseWhileRequestInFlight(t *testing.T) {
	listener := NewListener()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, sip.Serve(ctx, listener, pingSource{}, sip.WithMaxConnections(1000)))
	for i := range 1000 {
		conn, err := sip.Dial(ctx, "", "", sip.WithDial(func(context.Context, string, string) (io.ReadWriteCloser, error) {
			return listener.Dial("", "")
		}))
		require.NoError(t, err)
		pingCtx, pingCancel := context.WithTimeout(ctx, time.Second)
		done := make(chan struct{})
		go func() { _ = conn.Ping(pingCtx); close(done) }()
		time.Sleep(time.Duration(i%50) * 10 * time.Microsecond)
		_ = conn.Close()
		<-done
		pingCancel()
	}
}
