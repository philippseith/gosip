// Package ext provides optional integrations for gosip, including a restricted SSH forwarding server.
package ext

import (
	"context"
	"errors"
	"fmt"
	"net"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/joomcode/errorx"
)

var ErrInvalidSSHServerConfig = errors.New("invalid SSH server configuration")

// SSHServerOption configures an SSHServer.
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

// LoginCallback checks each SSH authentication attempt. ctx is scoped to the
// SSH connection, shared by that connection's callbacks, and canceled when the
// connection closes. It has no per-attempt deadline; derive a timeout for
// blocking external checks, return false when canceled, and do not retain ctx.
type LoginCallback func(context.Context, LoginInput) bool

// TargetRequest is the authenticated user's requested forwarding destination.
type TargetRequest struct {
	Username string
	Host     string
	Port     uint32
}

// TargetCallback checks each SSH local-forwarding destination. ctx has the SSH
// connection lifetime, not an individual channel lifetime, and no per-check
// deadline. Use it for context-aware checks, derive a timeout for blocking
// external work, return false when canceled, and do not retain ctx.
type TargetCallback func(context.Context, TargetRequest) bool

// forwardingMux accepts only direct-tcpip channels approved by the target callback.
type forwardingMux struct {
	targetCheck TargetCallback
}

// SSHServer accepts authenticated local TCP forwarding approved by its callbacks.
type SSHServer struct {
	server *gliderssh.Server
}

// WithLoginCallback sets the callback used for password and public-key authentication.
func WithLoginCallback(callback LoginCallback) SSHServerOption {
	return func(config *sshServerConfig) error {
		if callback == nil {
			return invalidSSHConfig("login callback must be non-nil")
		}
		config.loginCheck = callback
		return nil
	}
}

// WithTargetCallback sets the callback used for every local-forwarding destination.
func WithTargetCallback(callback TargetCallback) SSHServerOption {
	return func(config *sshServerConfig) error {
		if callback == nil {
			return invalidSSHConfig("target callback must be non-nil")
		}
		config.targetCheck = callback
		return nil
	}
}

// WithHostSigner configures an SSH server host key. At least one signer is required.
func WithHostSigner(signer gliderssh.Signer) SSHServerOption {
	return func(config *sshServerConfig) error {
		if signer == nil || signer.PublicKey() == nil {
			return invalidSSHConfig("host signer must be non-nil")
		}
		config.hostKeys = append(config.hostKeys, signer)
		return nil
	}
}

// NewSSHServer creates a server that accepts only authenticated direct-tcpip
// channels approved by its login and target callbacks. Session channels, remote
// forwarding, and other SSH channel or global request types are rejected. A host
// signer and both callbacks are required.
func NewSSHServer(options ...SSHServerOption) (*SSHServer, error) {
	config := sshServerConfig{}
	for _, option := range options {
		if option == nil {
			return nil, invalidSSHConfig("nil SSH server option")
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	if len(config.hostKeys) == 0 {
		return nil, invalidSSHConfig("at least one host signer is required")
	}
	if config.loginCheck == nil {
		return nil, invalidSSHConfig("login callback is required")
	}
	if config.targetCheck == nil {
		return nil, invalidSSHConfig("target callback is required")
	}

	mux := &forwardingMux{targetCheck: config.targetCheck}
	server := &gliderssh.Server{
		HostSigners: config.hostKeys,
		PasswordHandler: func(ctx gliderssh.Context, password string) bool {
			return config.loginCheck(ctx, LoginInput{
				Username: ctx.User(),
				Method:   LoginPassword,
				Password: password,
			})
		},
		PublicKeyHandler: func(ctx gliderssh.Context, key gliderssh.PublicKey) bool {
			return config.loginCheck(ctx, LoginInput{
				Username:  ctx.User(),
				Method:    LoginPublicKey,
				PublicKey: key,
			})
		},
		LocalPortForwardingCallback: mux.allowLocalTarget,
		ReversePortForwardingCallback: func(gliderssh.Context, string, uint32) bool {
			return false
		},
		ChannelHandlers: map[string]gliderssh.ChannelHandler{
			"direct-tcpip": gliderssh.DirectTCPIPHandler,
		},
		RequestHandlers:   map[string]gliderssh.RequestHandler{},
		SubsystemHandlers: map[string]gliderssh.SubsystemHandler{},
	}
	return &SSHServer{server: server}, nil
}

// Serve accepts SSH connections on listener until it is closed or the server is shut down.
func (s *SSHServer) Serve(listener net.Listener) error {
	if listener == nil {
		return invalidSSHConfig("listener must be non-nil")
	}
	return s.server.Serve(listener)
}

// Shutdown gracefully closes listeners and waits for active SSH connections to finish.
func (s *SSHServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Close immediately closes active listeners and SSH connections.
func (s *SSHServer) Close() error {
	return s.server.Close()
}

func (m *forwardingMux) allowLocalTarget(ctx gliderssh.Context, host string, port uint32) bool {
	return m.targetCheck(ctx, TargetRequest{
		Username: ctx.User(),
		Host:     host,
		Port:     port,
	})
}

func invalidSSHConfig(message string) error {
	return errorx.EnsureStackTrace(fmt.Errorf("%w: %s", ErrInvalidSSHServerConfig, message))
}
