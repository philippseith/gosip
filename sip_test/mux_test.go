package sip_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_MuxPing(t *testing.T) {
	mux := sip.NewMux(SimpleSyncClient{})
	err := mux.Ping()
	assert.NoError(t, err)
}

func TestMuxServe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := newCountedSyncClient(25 * time.Millisecond)
	mux := sip.NewMux(source)

	listener, err := net.Listen("tcp", "127.0.0.1:8086")
	require.NoError(t, err)
	defer func() {
		cancel()
		_ = listener.Close()
	}()

	go sip.Serve(ctx, listener, mux)

	assert.NoError(t, err)
	clientA, err := sip.NewClient("tcp", "127.0.0.1:8086")
	assert.NoError(t, err)
	defer clientA.Close()
	clientB, err := sip.NewClient("tcp", "127.0.0.1:8086")
	assert.NoError(t, err)
	defer clientB.Close()

	assert.NoError(t, clientA.Ping())
	assert.NoError(t, clientB.Ping())

	done := make(chan *sip.ReadEverythingResponse, 2)
	go func() {
		response, err := clientA.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		done <- &response
	}()
	go func() {
		response, err := clientB.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		done <- &response
	}()

	r1 := <-done
	r2 := <-done
	assert.Equal(t, r1, r2)
	assert.Equal(t, 1, source.callCount(0))

	go func() {
		response, err := clientA.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		done <- &response
	}()
	go func() {
		response, err := clientB.ReadEverything(0, 0, 1)
		assert.NoError(t, err)
		done <- &response
	}()

	r1 = <-done
	r2 = <-done
	assert.Equal(t, 2, source.callCount(0))
	assert.Equal(t, 1, source.callCount(1))
}

func TestMuxServeCoalescesConcurrentReads(t *testing.T) {
	listener := NewListener()
	source := newCountedSyncClient(0)
	firstRelease := make(chan struct{})
	source.setRelease(firstRelease)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, sip.Serve(ctx, listener, sip.NewMux(source)))

	clientA, _ := newInMemoryMuxClient(t, listener)
	defer clientA.Close()
	clientB, observedB := newInMemoryMuxClient(t, listener)
	defer clientB.Close()
	require.NoError(t, clientA.Ping())
	require.NoError(t, clientB.Ping())

	done := make(chan muxReadResult, 2)
	startMuxRead(clientA, 7, done)
	assert.Equal(t, uint32(7), waitForReadStart(t, source.started))
	startMuxRead(clientB, 7, done)
	waitForReadEverythingWrite(t, observedB.readEverythingWritten)
	time.Sleep(20 * time.Millisecond) // Keep the first source call in flight while the second stream is dispatched.
	assert.Equal(t, 1, source.callCount(7))
	close(firstRelease)

	first, second := <-done, <-done
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.Equal(t, first.response, second.response)
	assert.Equal(t, 1, source.callCount(7))

	differentRelease := make(chan struct{})
	source.setRelease(differentRelease)
	startMuxRead(clientA, 20, done)
	assert.Equal(t, uint32(20), waitForReadStart(t, source.started))
	startMuxRead(clientB, 21, done)
	assert.Equal(t, uint32(21), waitForReadStart(t, source.started))
	close(differentRelease)
	first, second = <-done, <-done
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.Equal(t, 1, source.callCount(20))
	assert.Equal(t, 1, source.callCount(21))

	repeatRelease := make(chan struct{})
	source.setRelease(repeatRelease)
	startMuxRead(clientA, 7, done)
	assert.Equal(t, uint32(7), waitForReadStart(t, source.started))
	close(repeatRelease)
	repeated := <-done
	require.NoError(t, repeated.err)
	assert.Equal(t, 2, source.callCount(7))
}

type countedSyncClient struct {
	mu      sync.Mutex
	calls   map[uint32]int
	started chan uint32
	release <-chan struct{}
	delay   time.Duration
}

func newCountedSyncClient(delay time.Duration) *countedSyncClient {
	return &countedSyncClient{
		calls:   make(map[uint32]int),
		started: make(chan uint32, 16),
		delay:   delay,
	}
}

func (s *countedSyncClient) Ping(...sip.RequestOption) error { return nil }

func (s *countedSyncClient) ReadEverything(_, _ int, idn uint32, _ ...sip.RequestOption) (sip.ReadEverythingResponse, error) {
	s.mu.Lock()
	s.calls[idn]++
	call := s.calls[idn]
	started, release, delay := s.started, s.release, s.delay
	s.mu.Unlock()

	if started != nil {
		started <- idn
	}
	if release != nil {
		<-release
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return sip.ReadEverythingResponse{Data: []byte{byte(idn), byte(call)}}, nil
}

func (s *countedSyncClient) ReadOnlyData(int, int, uint32, ...sip.RequestOption) (sip.ReadOnlyDataResponse, error) {
	return sip.ReadOnlyDataResponse{}, nil
}

func (s *countedSyncClient) ReadDescription(int, int, uint32, ...sip.RequestOption) (sip.ReadDescriptionResponse, error) {
	return sip.ReadDescriptionResponse{}, nil
}

func (s *countedSyncClient) ReadDataState(int, int, uint32, ...sip.RequestOption) (sip.ReadDataStateResponse, error) {
	return sip.ReadDataStateResponse{}, nil
}

func (s *countedSyncClient) WriteData(int, int, uint32, []byte, ...sip.RequestOption) error {
	return nil
}

func (s *countedSyncClient) callCount(idn uint32) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[idn]
}

func (s *countedSyncClient) setRelease(release <-chan struct{}) {
	s.mu.Lock()
	s.release = release
	s.mu.Unlock()
}

type muxReadResult struct {
	response sip.ReadEverythingResponse
	err      error
}

func startMuxRead(client sip.Client, idn uint32, done chan<- muxReadResult) {
	go func() {
		response, err := client.ReadEverything(0, 0, idn)
		done <- muxReadResult{response: response, err: err}
	}()
}

type requestObservingConn struct {
	net.Conn
	readEverythingWritten chan struct{}
}

func (c *requestObservingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err == nil && n == len(p) && len(p) >= 8 && binary.LittleEndian.Uint32(p[4:8]) == uint32(sip.ReadEverythingRequestMsgType) {
		c.readEverythingWritten <- struct{}{}
	}
	return n, err
}

func newInMemoryMuxClient(t *testing.T, listener *Listener) (sip.Client, *requestObservingConn) {
	t.Helper()
	observed := &requestObservingConn{readEverythingWritten: make(chan struct{}, 8)}
	client, err := sip.NewClient("tcp", "in-memory", sip.WithDial(func(_ context.Context, network, address string) (io.ReadWriteCloser, error) {
		conn, err := listener.Dial(network, address)
		if err != nil {
			return nil, err
		}
		observed.Conn = conn
		return observed, nil
	}))
	require.NoError(t, err)
	return client, observed
}

func waitForReadStart(t *testing.T, started <-chan uint32) uint32 {
	t.Helper()
	select {
	case idn := <-started:
		return idn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for source read")
		return 0
	}
}

func waitForReadEverythingWrite(t *testing.T, written <-chan struct{}) {
	t.Helper()
	select {
	case <-written:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for downstream read request")
	}
}
