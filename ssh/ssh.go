// Package ext provides optional integrations for gosip.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"sync"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/joomcode/errorx"
	"github.com/philippseith/gosip/sip"
	gossh "golang.org/x/crypto/ssh"
)

var ErrInvalidSIPMuxInstall = errors.New("invalid S/IP mux installation")

// DefaultMaxChannelsPerConnection is the number of concurrently open S/IP
// channels one SSH connection may hold unless WithMaxChannelsPerConnection
// says otherwise.
const DefaultMaxChannelsPerConnection = 32

// AttachOption configures AttachSIPSource.
type AttachOption func(*attachConfig) error

type attachConfig struct {
	maxChannelsPerConnection int
}

// WithMaxChannelsPerConnection limits how many direct-tcpip channels one SSH
// connection may have open at the same time. Every channel runs its own
// sip.Serve loop (with its own connection limit) and bridge, which costs five
// goroutines, so without this limit a single SSH connection can open an
// unbounded number of them. Channels beyond the limit are rejected with
// ResourceShortage and the limit is released when a channel closes. n must be
// greater than 0. The default is DefaultMaxChannelsPerConnection.
func WithMaxChannelsPerConnection(n int) AttachOption {
	return func(c *attachConfig) error {
		if n < 1 {
			return invalidSIPSourceInstall("max channels per connection must be greater than 0")
		}
		c.maxChannelsPerConnection = n
		return nil
	}
}

// TargetRequest identifies an SSH client's requested direct-tcpip destination.
type TargetRequest struct {
	Username string
	Host     string
	Port     uint32
}

// SIPSourceFactory selects the SyncClient used for one SSH forwarding target.
// Return an error to reject the target. The returned client may be a source or
// a mux such as sip.NewMux(source). The factory may be called concurrently.
// ctx is a standard context.Context derived from the SSH connection context.
type SIPSourceFactory func(ctx context.Context, target TargetRequest) (sip.SyncClient, error)

// AttachSIPSource adds S/IP handling to a caller-owned gliderlabs SSH server.
// Each authorized direct-tcpip channel gets a source from sourceFactory and a
// per-channel sip.Serve loop. connOptions are passed to each Serve call. The
// supplied context and the SSH connection context control that call's lifetime.
//
// Configure SSH authentication directly on server. Authentication is not
// required by this attachment: gliderlabs allows unauthenticated clients when
// no authentication handlers are configured. Do not treat TargetRequest.Username
// as an authenticated identity in that case. A pre-existing
// LocalPortForwardingCallback remains an additional filter. Other handlers are
// preserved; a pre-existing direct-tcpip handler is rejected. Call
// AttachSIPSource before server.Serve.
//
// The number of open channels per SSH connection is limited, see
// WithMaxChannelsPerConnection.
func AttachSIPSource(
	ctx context.Context,
	server *gliderssh.Server,
	sourceFactory SIPSourceFactory,
	connOptions []sip.ConnOption,
	attachOptions ...AttachOption,
) error {
	if ctx == nil {
		return invalidSIPSourceInstall("context must be non-nil")
	}
	if err := ctx.Err(); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if server == nil {
		return invalidSIPSourceInstall("SSH server must be non-nil")
	}
	if sourceFactory == nil {
		return invalidSIPSourceInstall("S/IP source factory must be non-nil")
	}
	if server.ChannelHandlers != nil && server.ChannelHandlers["direct-tcpip"] != nil {
		return invalidSIPSourceInstall("direct-tcpip already has a channel handler")
	}
	for _, option := range connOptions {
		if option == nil {
			return invalidSIPSourceInstall("ConnOption must be non-nil")
		}
	}

	config := attachConfig{maxChannelsPerConnection: DefaultMaxChannelsPerConnection}
	for _, option := range attachOptions {
		if option == nil {
			return invalidSIPSourceInstall("AttachOption must be non-nil")
		}
		if err := option(&config); err != nil {
			return err
		}
	}

	options := append([]sip.ConnOption(nil), connOptions...)
	if server.ChannelHandlers == nil {
		server.ChannelHandlers = make(map[string]gliderssh.ChannelHandler, len(gliderssh.DefaultChannelHandlers)+1)
		maps.Copy(server.ChannelHandlers, gliderssh.DefaultChannelHandlers)
	}
	server.ChannelHandlers["direct-tcpip"] = sipChannelHandler(ctx, sourceFactory, options, config.maxChannelsPerConnection)
	return nil
}

func sipChannelHandler(
	attachCtx context.Context,
	sourceFactory SIPSourceFactory,
	connOptions []sip.ConnOption,
	maxChannels int,
) gliderssh.ChannelHandler {
	return func(server *gliderssh.Server, _ *gossh.ServerConn, newChannel gossh.NewChannel, sshCtx gliderssh.Context) {
		var destination sipForwardChannelData
		if err := gossh.Unmarshal(newChannel.ExtraData(), &destination); err != nil {
			_ = newChannel.Reject(gossh.ConnectionFailed, "invalid forwarding request")
			return
		}
		if server.LocalPortForwardingCallback != nil && !server.LocalPortForwardingCallback(sshCtx, destination.DestAddr, destination.DestPort) {
			_ = newChannel.Reject(gossh.Prohibited, "port forwarding is disabled")
			return
		}

		releaseChannel, ok := acquireChannel(sshCtx, maxChannels)
		if !ok {
			_ = newChannel.Reject(gossh.ResourceShortage, "too many channels")
			return
		}
		serveCtx, cancel := context.WithCancel(sshCtx)
		stopAttachCancel := context.AfterFunc(attachCtx, cancel)
		listener := newSIPChannelListener()
		closeService := func() {
			stopAttachCancel()
			cancel()
			_ = listener.Close()
			releaseChannel()
		}
		target := TargetRequest{Username: sshCtx.User(), Host: destination.DestAddr, Port: destination.DestPort}
		source, err := sourceFactory(serveCtx, target)
		if err != nil || source == nil {
			closeService()
			_ = newChannel.Reject(gossh.Prohibited, "forwarding target rejected")
			return
		}
		if err := serveCtx.Err(); err != nil {
			closeService()
			_ = newChannel.Reject(gossh.ConnectionFailed, "SSH connection closed")
			return
		}
		if err := sip.Serve(serveCtx, listener, source, connOptions...); err != nil {
			closeService()
			_ = newChannel.Reject(gossh.ConnectionFailed, "S/IP service unavailable")
			return
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			closeService()
			return
		}
		go gossh.DiscardRequests(requests)

		serveConn, bridgeConn := net.Pipe()
		if err := listener.deliver(serveConn); err != nil {
			_ = serveConn.Close()
			_ = bridgeConn.Close()
			_ = channel.Close()
			closeService()
			return
		}
		bridgeSSHChannel(channel, bridgeConn, closeService)
	}
}

type channelCountKey struct{}

// acquireChannel counts one open channel for the SSH connection of ctx. It
// reports false when the connection already has limit channels open. The
// returned release function is idempotent.
func acquireChannel(ctx gliderssh.Context, limit int) (release func(), ok bool) {
	ctx.Lock()
	defer ctx.Unlock()
	count, _ := ctx.Value(channelCountKey{}).(*int)
	if count == nil {
		count = new(int)
		ctx.SetValue(channelCountKey{}, count)
	}
	if *count >= limit {
		return nil, false
	}
	*count++
	var once sync.Once
	return func() {
		once.Do(func() {
			ctx.Lock()
			defer ctx.Unlock()
			*count--
		})
	}, true
}

type sipChannelListener struct {
	incoming chan net.Conn
	closed   chan struct{}
	close    sync.Once
}

func newSIPChannelListener() *sipChannelListener {
	return &sipChannelListener{
		incoming: make(chan net.Conn),
		closed:   make(chan struct{}),
	}
}

func (l *sipChannelListener) Accept() (net.Conn, error) {
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	default:
	}
	select {
	case conn := <-l.incoming:
		select {
		case <-l.closed:
			_ = conn.Close()
			return nil, net.ErrClosed
		default:
			return conn, nil
		}
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *sipChannelListener) Close() error {
	l.close.Do(func() { close(l.closed) })
	return nil
}

func (l *sipChannelListener) Addr() net.Addr { return sipChannelAddr{} }

func (l *sipChannelListener) deliver(conn net.Conn) error {
	select {
	case <-l.closed:
		return net.ErrClosed
	case l.incoming <- conn:
		return nil
	}
}

type sipChannelAddr struct{}

func (sipChannelAddr) Network() string { return "ssh" }
func (sipChannelAddr) String() string  { return "direct-tcpip" }

type sipForwardChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func bridgeSSHChannel(channel gossh.Channel, conn net.Conn, onClose func()) {
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = channel.Close()
			_ = conn.Close()
			onClose()
		})
	}
	go func() {
		_, _ = io.Copy(channel, conn)
		closeBoth()
	}()
	go func() {
		_, _ = io.Copy(conn, channel)
		closeBoth()
	}()
}

func invalidSIPSourceInstall(message string) error {
	return errorx.EnsureStackTrace(fmt.Errorf("%w: %s", ErrInvalidSIPMuxInstall, message))
}
