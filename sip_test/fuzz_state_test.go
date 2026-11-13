package sip_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
)

// pingSource is a minimal trusted source for server-side fuzzing.
type pingSource struct{ SimpleSyncClient }

func (pingSource) ReadEverything(int, int, uint32, ...sip.RequestOption) (sip.ReadEverythingResponse, error) {
	return sip.ReadEverythingResponse{}, nil
}

// waitGoroutines waits up to d for the goroutine count to fall back to limit.
func waitGoroutines(limit int, d time.Duration) int {
	deadline := time.Now().Add(d)
	for {
		n := runtime.NumGoroutine()
		if n <= limit || time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func connectFrame() []byte {
	return append(le(uint32(1), uint32(sip.ConnectRequestMsgType)), le(uint32(1), uint32(200), uint32(1000))...)
}

func pingFrame() []byte { return le(uint32(7), uint32(sip.PingRequestMsgType)) }

// FuzzServeFrames writes arbitrary bytes behind a valid Connect to sip.Serve.
// Oracle: no panic, all server goroutines are gone 3 s after the connection and
// the server context are closed, and the server still answers a Ping from a new client
// before it is shut down.
func FuzzServeFrames(f *testing.F) {
	f.Add([]byte{})
	f.Add(pingFrame())
	f.Add(le(uint32(2), uint32(sip.ReadEverythingRequestMsgType), uint16(0), uint16(0), uint32(1)))
	f.Add(le(uint32(3), uint32(sip.WriteDataRequestMsgType), uint16(0), uint16(0), uint32(1), uint32(0xFFFFFFFF)))
	f.Add(le(uint32(4), uint32(0xFFFFFFFF)))
	f.Add(append(connectFrame(), pingFrame()...))
	f.Fuzz(func(t *testing.T, data []byte) {
		baseline := runtime.NumGoroutine()
		listener := NewListener()
		ctx, cancel := context.WithCancel(context.Background())
		if err := sip.Serve(ctx, listener, pingSource{}, sip.WithBusyTimeout(200), sip.WithLeaseTimeout(1000)); err != nil {
			t.Fatal(err)
		}

		attacker, err := listener.Dial("", "")
		if err != nil {
			t.Fatal(err)
		}
		_ = attacker.SetDeadline(time.Now().Add(2 * time.Second))
		drained := make(chan struct{})
		go func() { _, _ = io.Copy(io.Discard, attacker); close(drained) }()
		_, _ = attacker.Write(connectFrame())
		_, _ = attacker.Write(data)
		_ = attacker.Close()
		<-drained

		// The server must stay usable for a well-behaved client.
		client, err := sip.Dial(context.Background(), "", "", sip.WithBusyTimeout(200), sip.WithDial(
			func(context.Context, string, string) (io.ReadWriteCloser, error) { return listener.Dial("", "") }))
		if err != nil {
			t.Fatalf("server not usable after hostile input %x: %v", data, err)
		}
		pingCtx, pingCancel := context.WithTimeout(context.Background(), time.Second)
		err = client.Ping(pingCtx)
		pingCancel()
		if err != nil {
			t.Fatalf("ping after hostile input %x: %v", data, err)
		}
		_ = client.Close()

		cancel()
		if n := waitGoroutines(baseline, 3*time.Second); n > baseline {
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]
			t.Fatalf("goroutines: %d before, %d after for input %x\n%s", baseline, n, data, buf)
		}
	})
}

// FuzzClientAgainstHostileServer feeds arbitrary bytes as the server's byte stream
// to sip.Dial and the public Conn calls. Oracle: every call returns before its
// context deadline plus 1 s (250 ms deadlines), no panic, no goroutine left 3 s after Close.
func FuzzClientAgainstHostileServer(f *testing.F) {
	connectResp := func(busy uint32, types ...uint32) []byte {
		b := le(uint32(1), uint32(sip.ConnectResponseMsgType), uint32(1), busy, uint32(1000), uint32(len(types)))
		return append(b, le(types)...)
	}
	f.Add(connectResp(200, 65, 69))
	f.Add(connectResp(0))
	f.Add(connectResp(0xFFFFFFFF))
	f.Add(append(connectResp(200, 65), le(uint32(2), uint32(sip.PingResponseMsgType))...))
	f.Add(append(connectResp(200, 65), le(uint32(2), uint32(sip.ReadEverythingResponseMsgType), uint16(0), uint16(0), uint16(0), uint32(0), uint16(0), [8]byte{}, [8]byte{}, uint32(0), uint32(0xFFFF))...))
	f.Add(append(connectResp(200, 65), le(uint32(2), uint32(sip.ExceptionMsgType), uint16(1), uint32(1))...))
	f.Add(append(connectResp(200, 65), le(uint32(2), uint32(sip.BusyResponseMsgType))...))
	f.Fuzz(func(t *testing.T, data []byte) {
		baseline := runtime.NumGoroutine()
		clientSide, serverSide := net.Pipe()
		go func() {
			defer serverSide.Close()
			_ = serverSide.SetDeadline(time.Now().Add(5 * time.Second))
			// Read the connect request (8 byte header + 12 byte body) so the client can finish writing.
			req := make([]byte, 20)
			if _, err := io.ReadFull(serverSide, req); err != nil {
				return
			}
			// The hostile stream is replayed with the connect request's transaction ID,
			// so that fuzzed frames can match an open transaction.
			stream := bytes.Clone(data)
			if len(stream) >= 4 {
				copy(stream[:4], req[:4])
			}
			_, _ = serverSide.Write(stream)
			_, _ = io.Copy(io.Discard, serverSide)
		}()

		call := func(name string, do func(ctx context.Context) error) {
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			start := time.Now()
			_ = do(ctx)
			if elapsed := time.Since(start); elapsed > 1250*time.Millisecond {
				t.Fatalf("%s took %v (deadline 250ms) for stream %x", name, elapsed, data)
			}
		}

		dialCtx, dialCancel := context.WithTimeout(context.Background(), time.Second)
		start := time.Now()
		conn, err := sip.Dial(dialCtx, "", "", sip.WithBusyTimeout(200), sip.WithDial(
			func(context.Context, string, string) (io.ReadWriteCloser, error) { return clientSide, nil }))
		dialCancel()
		if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
			t.Fatalf("Dial took %v (deadline 1s) for stream %x", elapsed, data)
		}
		if err == nil {
			call("Ping", conn.Ping)
			call("ReadEverything", func(ctx context.Context) error { _, err := conn.ReadEverything(ctx, 0, 0, 1); return err })
			call("ReadOnlyData", func(ctx context.Context) error { _, err := conn.ReadOnlyData(ctx, 0, 0, 1); return err })
			call("ReadDescription", func(ctx context.Context) error { _, err := conn.ReadDescription(ctx, 0, 0, 1); return err })
			call("ReadDataState", func(ctx context.Context) error { _, err := conn.ReadDataState(ctx, 0, 0, 1); return err })
			call("WriteData", func(ctx context.Context) error { return conn.WriteData(ctx, 0, 0, 1, []byte{1}) })
			_ = conn.Close()
		}
		_ = clientSide.Close()
		if n := waitGoroutines(baseline, 3*time.Second); n > baseline {
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]
			t.Fatalf("goroutines: %d before, %d after for stream %x\n%s", baseline, n, data, buf)
		}
	})
}
