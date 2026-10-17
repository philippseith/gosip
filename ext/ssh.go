// Package ext provides optional integrations for gosip.
package ext

import (
	"errors"
	"fmt"
	"net"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/joomcode/errorx"
)

var ErrInvalidSIPMuxInstall = errors.New("invalid S/IP mux installation")

// InstallSIPMux installs the S/IP listener behind a caller-owned SSH server.
// It creates a loopback TCP listener, passes it to serve, and returns the
// listener so the caller can inspect its address and manage its lifetime.
//
// Configure authentication and any SSH callbacks directly on server. If no
// LocalPortForwardingCallback is set, only this listener's exact loopback
// address is allowed. An existing callback is preserved and remains
// responsible for authorizing this and any other forwarding targets.
//
// The Serve callback can choose the mux, its SyncClient, and all sip.Serve
// options. For example, call sip.Serve(ctx, listener, sip.NewMux(source),
// options...). InstallSIPMux must run before server.Serve. If ChannelHandlers
// is nil, it enables only direct-tcpip; preconfigured handlers are preserved.
func InstallSIPMux(server *gliderssh.Server, serve func(net.Listener) error) (net.Listener, error) {
	if server == nil {
		return nil, invalidSIPMuxInstall("SSH server must be non-nil")
	}
	if len(server.HostSigners) == 0 {
		return nil, invalidSIPMuxInstall("at least one host signer is required")
	}
	if server.PasswordHandler == nil && server.PublicKeyHandler == nil && server.KeyboardInteractiveHandler == nil {
		return nil, invalidSIPMuxInstall("at least one SSH authentication callback is required")
	}
	if serve == nil {
		return nil, invalidSIPMuxInstall("S/IP Serve callback must be non-nil")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	if err := serve(listener); err != nil {
		_ = listener.Close()
		return nil, errorx.EnsureStackTrace(err)
	}

	if server.ChannelHandlers == nil {
		server.ChannelHandlers = make(map[string]gliderssh.ChannelHandler)
	}
	if server.ChannelHandlers["direct-tcpip"] == nil {
		server.ChannelHandlers["direct-tcpip"] = gliderssh.DirectTCPIPHandler
	}
	if server.LocalPortForwardingCallback == nil {
		addr := listener.Addr().(*net.TCPAddr)
		targetHost := addr.IP.String()
		targetPort := uint32(addr.Port)
		server.LocalPortForwardingCallback = func(_ gliderssh.Context, host string, port uint32) bool {
			return host == targetHost && port == targetPort
		}
	}

	return listener, nil
}

func invalidSIPMuxInstall(message string) error {
	return errorx.EnsureStackTrace(fmt.Errorf("%w: %s", ErrInvalidSIPMuxInstall, message))
}
