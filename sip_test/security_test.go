package sip_test

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/require"
)

func TestCanceledWriteDataDoesNotReplay(t *testing.T) {
	listener := NewListener()
	serveCtx, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()

	source := &blockingWriteSource{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	defer func() {
		select {
		case <-source.release:
		default:
			close(source.release)
		}
	}()
	require.NoError(t, sip.Serve(serveCtx, listener, source))

	client, err := sip.Dial(context.Background(), "", "", sip.WithDial(func(ctx context.Context, network, address string) (io.ReadWriteCloser, error) {
		return listener.Dial(network, address)
	}))
	require.NoError(t, err)
	defer client.Close()

	writeCtx, cancelWrite := context.WithCancel(context.Background())
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- client.WriteData(writeCtx, 0, 0, 1, []byte{1})
	}()

	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for WriteData to reach the server")
	}
	cancelWrite()
	select {
	case err := <-writeDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled WriteData did not return")
	}

	close(source.release)
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelPing()
	require.NoError(t, client.Ping(pingCtx), "response dispatch must continue after cancellation")
	require.EqualValues(t, 1, source.writes.Load(), "canceled WriteData must not be replayed")
}

type blockingWriteSource struct {
	SimpleSyncClient
	started chan struct{}
	release chan struct{}
	writes  atomic.Int32
}

func (s *blockingWriteSource) WriteData(int, int, uint32, []byte, ...sip.RequestOption) error {
	if s.writes.Add(1) == 1 {
		close(s.started)
	}
	<-s.release
	return nil
}
