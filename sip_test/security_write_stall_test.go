package sip_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/require"
)

// A peer that connects but never reads the response must not hold a server
// connection slot forever. The server sets no write deadline: its Write blocks
// until the peer reads or closes. With the synchronous in-memory pipe this is
// immediate; over TCP the kernel buffers must fill first (on macOS loopback more
// than 4 MB of 8 byte pings did not stall a connection within 4 s).
func TestServeSlowReaderDoesNotHoldConnectionSlotForever(t *testing.T) {
	listener := NewListener()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, sip.Serve(ctx, listener, pingSource{},
		sip.WithMaxConnections(2), sip.WithBusyTimeout(300), sip.WithLeaseTimeout(1000)))

	for range 2 {
		conn, err := listener.Dial("", "")
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.Write(connectFrame()) // the response is never read
		require.NoError(t, err)
	}

	// Well after the busy timeout, a regular client must be served again.
	time.Sleep(time.Second)
	result := make(chan error, 1)
	go func() {
		client, err := sip.Dial(ctx, "", "", sip.WithBusyTimeout(300), sip.WithDial(
			func(context.Context, string, string) (io.ReadWriteCloser, error) { return listener.Dial("", "") }))
		if err == nil {
			pingCtx, pingCancel := context.WithTimeout(ctx, time.Second)
			err = client.Ping(pingCtx)
			pingCancel()
			_ = client.Close()
		}
		result <- err
	}()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("both server slots are still held by peers that never read their response")
	}
}
