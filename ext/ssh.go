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

// SSHServerOption configures SSH authentication, forwarding authorization, and host keys.
type SSHServerOption func(*sshServerConfig) error

type sshServerConfig struct {
	hostKeys    []gliderssh.Signer
	loginCheck  LoginCallback
	targetCheck TargetCallback
}

// LoginMethod identifies the SSH authentication method in LoginInput.
type LoginMethod uint8

const (
	LoginPassword LoginMethod = iota + 1
	LoginPublicKey
)

// LoginInput contains the client-supplied value for one authentication attempt.
// Password is set for LoginPassword; PublicKey is set for LoginPublicKey.
type LoginInput struct {
	Username  string
	Method    LoginMethod
	Password  string
	PublicKey gliderssh.PublicKey
}

// LoginCallback checks each SSH authentication attempt. Its context is scoped to
// the SSH connection, has no per-attempt deadline, and must not be retained.
type LoginCallback func(context.Context, LoginInput) bool

// TargetRequest identifies an SSH client's requested direct-tcpip destination.
type TargetRequest struct {
	Username string
	Host     string
	Port     uint32
}

// TargetCallback authorizes each SSH local-forwarding request. Its context is
// scoped to the SSH connection, has no per-request deadline, and must not be retained.
type TargetCallback func(context.Context, TargetRequest) bool

// WithHostSigner adds a host key to the caller-owned SSH server.
func WithHostSigner(signer gliderssh.Signer) SSHServerOption {
	return func(config *sshServerConfig) error {
		if signer == nil || signer.PublicKey() == nil {
			return invalidSIPMuxInstall("host signer must be non-nil")
		}
		config.hostKeys = append(config.hostKeys, signer)
		return nil
	}
}

// WithLoginCallback sets the callback used for password and public-key authentication.
func WithLoginCallback(callback LoginCallback) SSHServerOption {
	return func(config *sshServerConfig) error {
		if callback == nil {
			return invalidSIPMuxInstall("login callback must be non-nil")
		}
		config.loginCheck = callback
		return nil
	}
}

// WithTargetCallback sets the callback used for every S/IP direct-tcpip channel.
func WithTargetCallback(callback TargetCallback) SSHServerOption {
	return func(config *sshServerConfig) error {
		if callback == nil {
			return invalidSIPMuxInstall("target callback must be non-nil")
		}
		config.targetCheck = callback
		return nil
	}
}

// InstallSIPMux adds S/IP handling to a caller-owned gliderlabs SSH server.
// Each authorized direct-tcpip channel is delivered to sip.Serve as a net.Conn;
// no TCP listener or destination dial is created. ctx controls the Serve loop.
//
// The supplied SSH options configure host keys and callback-based login and
// target checks. Other SSH handlers remain untouched. A pre-existing
// direct-tcpip handler is rejected rather than silently replaced. Call this
// before server.Serve.
func InstallSIPMux(
	ctx context.Context,
	server *gliderssh.Server,
	mux sip.SyncClient,
	connOptions []sip.ConnOption,
	sshOptions ...SSHServerOption,
) error {
	if ctx == nil {
		return invalidSIPMuxInstall("context must be non-nil")
	}
	if err := ctx.Err(); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if server == nil {
		return invalidSIPMuxInstall("SSH server must be non-nil")
	}
	if mux == nil {
		return invalidSIPMuxInstall("S/IP mux must be non-nil")
	}
	if len(sshOptions) == 0 {
		return invalidSIPMuxInstall("SSH options are required")
	}

	config := sshServerConfig{}
	for _, option := range sshOptions {
		if option == nil {
			return invalidSIPMuxInstall("SSH option must be non-nil")
		}
		if err := option(&config); err != nil {
			return err
		}
	}
	if len(config.hostKeys) == 0 && len(server.HostSigners) == 0 {
		return invalidSIPMuxInstall("at least one host signer is required")
	}
	if config.loginCheck == nil && server.PasswordHandler == nil && server.PublicKeyHandler == nil && server.KeyboardInteractiveHandler == nil {
		return invalidSIPMuxInstall("an SSH authentication callback is required")
	}
	if config.targetCheck == nil && server.LocalPortForwardingCallback == nil {
		return invalidSIPMuxInstall("a target authorization callback is required")
	}
	if server.ChannelHandlers != nil && server.ChannelHandlers["direct-tcpip"] != nil {
		return invalidSIPMuxInstall("direct-tcpip already has a channel handler")
	}

	listener := newSIPChannelListener()
	if err := sip.Serve(ctx, listener, mux, connOptions...); err != nil {
		_ = listener.Close()
		return errorx.EnsureStackTrace(err)
	}

	for _, signer := range config.hostKeys {
		server.AddHostKey(signer)
	}
	if config.loginCheck != nil {
		server.PasswordHandler = func(ctx gliderssh.Context, password string) bool {
			return config.loginCheck(ctx, LoginInput{
				Username: ctx.User(),
				Method:   LoginPassword,
				Password: password,
			})
		}
		server.PublicKeyHandler = func(ctx gliderssh.Context, key gliderssh.PublicKey) bool {
			return config.loginCheck(ctx, LoginInput{
				Username:  ctx.User(),
				Method:    LoginPublicKey,
				PublicKey: key,
			})
		}
	}
	if config.targetCheck != nil {
		server.LocalPortForwardingCallback = func(ctx gliderssh.Context, host string, port uint32) bool {
			return config.targetCheck(ctx, TargetRequest{Username: ctx.User(), Host: host, Port: port})
		}
	}
	if server.ChannelHandlers == nil {
		server.ChannelHandlers = make(map[string]gliderssh.ChannelHandler)
	}
	server.ChannelHandlers["direct-tcpip"] = sipChannelHandler(listener)
	return nil
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

func sipChannelHandler(listener *sipChannelListener) gliderssh.ChannelHandler {
	return func(server *gliderssh.Server, _ *gossh.ServerConn, newChannel gossh.NewChannel, ctx gliderssh.Context) {
		var destination sipForwardChannelData
		if err := gossh.Unmarshal(newChannel.ExtraData(), &destination); err != nil {
			_ = newChannel.Reject(gossh.ConnectionFailed, "invalid forwarding request")
			return
		}
		if server.LocalPortForwardingCallback == nil || !server.LocalPortForwardingCallback(ctx, destination.DestAddr, destination.DestPort) {
			_ = newChannel.Reject(gossh.Prohibited, "port forwarding is disabled")
			return
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}
		go gossh.DiscardRequests(requests)

		serveConn, bridgeConn := net.Pipe()
		if err := listener.deliver(serveConn); err != nil {
			_ = serveConn.Close()
			_ = bridgeConn.Close()
			_ = channel.Close()
			return
		}
		bridgeSSHChannel(channel, bridgeConn)
	}
}

func bridgeSSHChannel(channel gossh.Channel, conn net.Conn) {
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = channel.Close()
			_ = conn.Close()
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

func invalidSIPMuxInstall(message string) error {
	return errorx.EnsureStackTrace(fmt.Errorf("%w: %s", ErrInvalidSIPMuxInstall, message))
}
