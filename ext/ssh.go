// Package ext provides optional integrations for gosip.
package ext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/joomcode/errorx"
	"github.com/philippseith/gosip/sip"
	gossh "golang.org/x/crypto/ssh"
)

var ErrInvalidSIPMuxInstall = errors.New("invalid S/IP mux installation")

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
// Configure SSH authentication directly on server. If its
// LocalPortForwardingCallback is set, that callback is an additional filter;
// sourceFactory can perform target selection and reject targets by returning an
// error. Other channel, request, and subsystem handlers are preserved. A
// pre-existing direct-tcpip handler is rejected rather than replaced. Call
// AttachSIPSource before server.Serve.
func AttachSIPSource(
	ctx context.Context,
	server *gliderssh.Server,
	sourceFactory SIPSourceFactory,
	connOptions []sip.ConnOption,
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
	if server.PasswordHandler == nil && server.PublicKeyHandler == nil && server.KeyboardInteractiveHandler == nil {
		return invalidSIPSourceInstall("configure SSH authentication on the server")
	}
	if server.ChannelHandlers != nil && server.ChannelHandlers["direct-tcpip"] != nil {
		return invalidSIPSourceInstall("direct-tcpip already has a channel handler")
	}
	for _, option := range connOptions {
		if option == nil {
			return invalidSIPSourceInstall("ConnOption must be non-nil")
		}
	}

	options := append([]sip.ConnOption(nil), connOptions...)
	if server.ChannelHandlers == nil {
		server.ChannelHandlers = make(map[string]gliderssh.ChannelHandler, len(gliderssh.DefaultChannelHandlers)+1)
		for channelType, handler := range gliderssh.DefaultChannelHandlers {
			server.ChannelHandlers[channelType] = handler
		}
	}
	server.ChannelHandlers["direct-tcpip"] = sipChannelHandler(ctx, sourceFactory, options)
	return nil
}

func sipChannelHandler(
	attachCtx context.Context,
	sourceFactory SIPSourceFactory,
	connOptions []sip.ConnOption,
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

		serveCtx, cancel := context.WithCancel(sshCtx)
		stopAttachCancel := context.AfterFunc(attachCtx, cancel)
		listener := newSIPChannelListener()
		closeService := func() {
			stopAttachCancel()
			cancel()
			_ = listener.Close()
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
