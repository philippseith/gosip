package sip_test

import (
	"context"
	"io"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/require"
)

// A failed Dial (here: the peer answers the Connect with an invalid message
// type) must release everything it started. Every failed attempt of a
// reconnecting Client runs through this path.
func TestFailedDialDoesNotLeakGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()
	const attempts = 20
	for range attempts {
		clientSide, serverSide := net.Pipe()
		go func() {
			defer serverSide.Close()
			req := make([]byte, 20)
			if _, err := io.ReadFull(serverSide, req); err != nil {
				return
			}
			_, _ = serverSide.Write(le(uint32(1), uint32(0xFFFF))) // header with an unknown message type
			_, _ = io.Copy(io.Discard, serverSide)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := sip.Dial(ctx, "", "", sip.WithBusyTimeout(200), sip.WithDial(
			func(context.Context, string, string) (io.ReadWriteCloser, error) { return clientSide, nil }))
		cancel()
		require.Error(t, err)
		require.Nil(t, conn)
	}
	n := waitGoroutines(baseline, 3*time.Second)
	t.Logf("goroutines: %d before, %d after %d failed dials", baseline, n, attempts)
	require.LessOrEqual(t, n, baseline, "failed dials leak goroutines")
}
