package ssh_test

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/philippseith/gosip/sip"
	"github.com/philippseith/gosip/ssh"
	"github.com/stretchr/testify/require"
	cryptossh "golang.org/x/crypto/ssh"
)

// forwardRequest is the direct-tcpip channel payload, RFC 4254 section 7.2.
type forwardRequest struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func openDirectTCPIP(client *cryptossh.Client, host string, port uint32) (cryptossh.Channel, error) {
	channel, requests, err := client.OpenChannel("direct-tcpip", cryptossh.Marshal(&forwardRequest{
		DestAddr: host, DestPort: port, OriginAddr: "127.0.0.1", OriginPort: 1,
	}))
	if err != nil {
		return nil, err
	}
	go cryptossh.DiscardRequests(requests)
	return channel, nil
}

func goroutinesBackTo(limit int, d time.Duration) int {
	deadline := time.Now().Add(d)
	for {
		n := runtime.NumGoroutine()
		if n <= limit || time.Now().After(deadline) {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// gosipGoroutines counts goroutines that run, or were started by, gosip server code.
func gosipGoroutines() int {
	buf := make([]byte, 64<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, stack := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(stack, "philippseith/gosip/ssh.") || strings.Contains(stack, "philippseith/gosip/sip.") {
			n++
		}
	}
	return n
}

func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func newUnauthenticatedServer(t *testing.T, factory ssh.SIPSourceFactory, options ...sip.ConnOption) (string, cryptossh.PublicKey) {
	t.Helper()
	hostSigner := generateSigner(t)
	server := &gliderssh.Server{HostSigners: []gliderssh.Signer{hostSigner}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, ssh.AttachSIPSource(ctx, server, factory, options))
	return startSSHServer(t, server), hostSigner.PublicKey()
}

// A server without authentication handlers accepts every client, and the
// factory sees the destination and user exactly as the attacker sent them,
// including a port above 65535 and a host that is not a host name. Factories
// must therefore validate and never use them as a dial address.
func TestSSHFactoryReceivesAttackerControlledTargetVerbatim(t *testing.T) {
	var got atomic.Pointer[ssh.TargetRequest]
	address, hostKey := newUnauthenticatedServer(t, func(_ context.Context, target ssh.TargetRequest) (sip.SyncClient, error) {
		got.Store(&target)
		return nil, errors.New("internal detail: db password is hunter2")
	})
	const evilHost = "../../etc/passwd\x00; rm -rf /\n"
	client := dialSSH(t, address, "root\x00admin", nil, hostKey)
	_, err := openDirectTCPIP(client, evilHost, 0xFFFFFFFF)
	require.Error(t, err)
	target := got.Load()
	require.NotNil(t, target, "factory must have been called without any authentication")
	t.Logf("factory received: %#v", *target)
	require.Equal(t, evilHost, target.Host)
	require.EqualValues(t, uint32(0xFFFFFFFF), target.Port)
	require.Equal(t, "root\x00admin", target.Username)
	// The client only learns the generic reason, not the factory's error text.
	require.NotContains(t, err.Error(), "hunter2")
	t.Logf("client-visible rejection: %v", err)
}

// LocalPortForwardingCallback must run before the factory, and a prohibition
// must not reach the factory at all.
func TestSSHPortForwardingCallbackRunsBeforeFactory(t *testing.T) {
	var factoryCalls atomic.Int32
	hostSigner := generateSigner(t)
	server := &gliderssh.Server{
		HostSigners: []gliderssh.Signer{hostSigner},
		LocalPortForwardingCallback: func(_ gliderssh.Context, host string, port uint32) bool {
			return host == "allowed"
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, ssh.AttachSIPSource(ctx, server, func(context.Context, ssh.TargetRequest) (sip.SyncClient, error) {
		factoryCalls.Add(1)
		return &countingSIPSource{}, nil
	}, nil))
	client := dialSSH(t, startSSHServer(t, server), "u", nil, hostSigner.PublicKey())
	_, err := openDirectTCPIP(client, "denied", 35021)
	require.Error(t, err)
	require.EqualValues(t, 0, factoryCalls.Load())
	t.Logf("callback rejection: %v", err)
	channel, err := openDirectTCPIP(client, "allowed", 35021)
	require.NoError(t, err)
	_ = channel.Close()
	require.EqualValues(t, 1, factoryCalls.Load())
}

// A factory that stalls on one channel must not block other channels of the
// same SSH connection (gliderlabs runs every channel handler in its own goroutine).
func TestSSHStalledFactoryDoesNotStarveOtherChannels(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	address, hostKey := newUnauthenticatedServer(t, func(ctx context.Context, target ssh.TargetRequest) (sip.SyncClient, error) {
		if calls.Add(1) == 1 {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return &countingSIPSource{}, nil
	})
	t.Cleanup(func() { close(release) })
	client := dialSSH(t, address, "u", nil, hostKey)
	stalled := make(chan error, 1)
	go func() { _, err := openDirectTCPIP(client, "h", 1); stalled <- err }()
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 2*time.Second, 5*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		sipClient, err := sip.NewClient("tcp", "h:1", sip.WithDial(func(context.Context, string, string) (io.ReadWriteCloser, error) {
			return client.Dial("tcp", "h:1")
		}))
		if err == nil {
			defer sipClient.Close()
			_, err = sipClient.ReadDataState(0, 0, 1)
		}
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("second channel starved by stalled factory call")
	}
}

// Closing the SSH connection must tear down every per-channel Serve loop and both
// io.Copy goroutines of the bridge.
func TestSSHTeardownLeavesNoGoroutines(t *testing.T) {
	address, hostKey := newUnauthenticatedServer(t, func(context.Context, ssh.TargetRequest) (sip.SyncClient, error) {
		return &countingSIPSource{}, nil
	}, sip.WithBusyTimeout(500))
	// Establish the baseline after server start, before any client connection.
	runtime.GC()
	baseline := runtime.NumGoroutine()

	for _, mode := range []string{"client-close", "channel-close", "half-close"} {
		client := dialSSH(t, address, "u", nil, hostKey)
		channel, err := openDirectTCPIP(client, "h", 1)
		require.NoError(t, err)
		// Valid Connect so that the server side is active.
		_, err = channel.Write(append(le32(1, uint32(sip.ConnectRequestMsgType)), le32(1, 200, 1000)...))
		require.NoError(t, err)
		resp := make([]byte, 8+16+6*4)
		_, err = io.ReadFull(channel, resp)
		require.NoError(t, err, mode)
		switch mode {
		case "client-close":
			require.NoError(t, client.Close())
		case "channel-close":
			require.NoError(t, channel.Close())
			require.NoError(t, client.Close())
		case "half-close":
			require.NoError(t, channel.CloseWrite())
			_, _ = io.Copy(io.Discard, channel)
			require.NoError(t, client.Close())
		}
		if n := goroutinesBackTo(baseline, 3*time.Second); n > baseline {
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]
			t.Fatalf("%s: goroutines %d -> %d\n%s", mode, baseline, n, buf)
		}
	}
}

func le32(vs ...uint32) []byte {
	b := make([]byte, 0, 4*len(vs))
	for _, v := range vs {
		b = append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	return b
}

// Cancelling the context given to AttachSIPSource must end channels that are
// already established.
func TestSSHAttachContextCancelEndsChannels(t *testing.T) {
	hostSigner := generateSigner(t)
	server := &gliderssh.Server{HostSigners: []gliderssh.Signer{hostSigner}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, ssh.AttachSIPSource(ctx, server, func(context.Context, ssh.TargetRequest) (sip.SyncClient, error) {
		return &countingSIPSource{}, nil
	}, []sip.ConnOption{sip.WithBusyTimeout(500)}))
	client := dialSSH(t, startSSHServer(t, server), "u", nil, hostSigner.PublicKey())
	channel, err := openDirectTCPIP(client, "h", 1)
	require.NoError(t, err)
	cancel()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, channel); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("channel still open 3 s after the attach context was cancelled")
	}
}

// Measures the cost of open channels. Every channel owns a net.Pipe, a Serve
// accept loop and two io.Copy goroutines; the 64 connection cap of sip.Serve is
// per channel (per listener), so only the per-connection channel limit bounds
// what one SSH connection can cost.
func TestSSHChannelFloodCost(t *testing.T) {
	if testing.Short() {
		t.Skip("resource measurement")
	}
	address, hostKey := newUnauthenticatedServer(t, func(context.Context, ssh.TargetRequest) (sip.SyncClient, error) {
		return &countingSIPSource{}, nil
	})
	for _, shape := range []struct {
		name           string
		conns, perConn int
	}{
		{"1 connection x 1000 channels", 1, 1000},
		{"100 connections x 10 channels", 100, 10},
	} {
		t.Run(shape.name, func(t *testing.T) {
			runtime.GC()
			time.Sleep(50 * time.Millisecond)
			baseG, baseHeap := runtime.NumGoroutine(), heapInUse()
			var (
				channels []cryptossh.Channel
				clients  []*cryptossh.Client
				mu       sync.Mutex
				wg       sync.WaitGroup
				failures atomic.Int32
			)
			for range shape.conns {
				client, err := cryptossh.Dial("tcp", address, clientConfig("u", nil, hostKey))
				require.NoError(t, err)
				clients = append(clients, client)
				for range shape.perConn {
					wg.Add(1)
					go func() {
						defer wg.Done()
						channel, err := openDirectTCPIP(client, "h", 1)
						if err != nil {
							failures.Add(1)
							return
						}
						mu.Lock()
						channels = append(channels, channel)
						mu.Unlock()
					}()
				}
			}
			wg.Wait()
			opened := len(channels)
			g, heap := runtime.NumGoroutine(), heapInUse()
			t.Logf("goroutines started by gosip server code: %d (%.1f per channel)", gosipGoroutines(), float64(gosipGoroutines())/float64(max(opened, 1)))
			t.Logf("%s: opened=%d rejected=%d goroutines +%d (%.1f per channel, client and server side together) heap +%.1f MB (%.0f KB per channel)",
				shape.name, opened, failures.Load(), g-baseG, float64(g-baseG)/float64(max(opened, 1)),
				float64(int64(heap)-int64(baseHeap))/1e6, float64(int64(heap)-int64(baseHeap))/1e3/float64(max(opened, 1)))
			for _, c := range clients {
				_ = c.Close()
			}
			if n := goroutinesBackTo(baseG, 5*time.Second); n > baseG {
				t.Errorf("goroutines did not return to baseline: %d -> %d", baseG, n)
			}
		})
	}
}
