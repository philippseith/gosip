// Package ext provides optional integrations for gosip, including a restricted SSH forwarding server.
package ext

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/joomcode/errorx"
)

var ErrInvalidSSHServerConfig = errors.New("invalid SSH server configuration")

// SSHServerOption configures an SSHServer.
type SSHServerOption func(*sshServerConfig) error

type sshServerConfig struct {
	targets    map[forwardTarget]struct{}
	passwords  map[string][]byte
	publicKeys map[string][]gliderssh.PublicKey
	hostKeys   []gliderssh.Signer
}

type forwardTarget struct {
	host string
	port uint16
}

// forwardingMux accepts only direct-tcpip channels targeting an explicit allowlist.
type forwardingMux struct {
	targets map[forwardTarget]struct{}
}

// SSHServer accepts authenticated local TCP forwarding to configured targets only.
type SSHServer struct {
	server *gliderssh.Server
}

// WithForwardingTargets allows SSH clients to open local-forwarding channels
// only to these exact host:port targets. Target host names are matched
// case-insensitively; IP addresses are matched by their canonical form.
func WithForwardingTargets(targets ...string) SSHServerOption {
	values := append([]string(nil), targets...)
	return func(config *sshServerConfig) error {
		if len(values) == 0 {
			return invalidSSHConfig("at least one forwarding target is required")
		}
		for _, value := range values {
			target, err := parseForwardTarget(value)
			if err != nil {
				return errorx.EnsureStackTrace(fmt.Errorf("%w: forwarding target %q: %w", ErrInvalidSSHServerConfig, value, err))
			}
			config.targets[target] = struct{}{}
		}
		return nil
	}
}

// WithPasswordCredential adds a username/password accepted for SSH client authentication.
func WithPasswordCredential(username, password string) SSHServerOption {
	return func(config *sshServerConfig) error {
		if username == "" || password == "" {
			return invalidSSHConfig("username and password must be non-empty")
		}
		config.passwords[username] = []byte(password)
		return nil
	}
}

// WithAuthorizedKey adds one OpenSSH authorized_keys entry for the given user.
func WithAuthorizedKey(username string, authorizedKey []byte) SSHServerOption {
	keyText := bytes.Clone(authorizedKey)
	return func(config *sshServerConfig) error {
		if username == "" || len(keyText) == 0 {
			return invalidSSHConfig("username and authorized key must be non-empty")
		}
		key, _, _, rest, err := gliderssh.ParseAuthorizedKey(keyText)
		if err != nil {
			return errorx.EnsureStackTrace(fmt.Errorf("%w: parse authorized key: %w", ErrInvalidSSHServerConfig, err))
		}
		if len(bytes.TrimSpace(rest)) != 0 {
			return invalidSSHConfig("WithAuthorizedKey accepts exactly one authorized_keys entry")
		}
		config.publicKeys[username] = append(config.publicKeys[username], key)
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
// channels to configured targets. Session channels, remote forwarding, and
// other SSH channel or global request types are rejected. A host signer, at
// least one client credential, and at least one forwarding target are required.
func NewSSHServer(options ...SSHServerOption) (*SSHServer, error) {
	config := sshServerConfig{
		targets:    make(map[forwardTarget]struct{}),
		passwords:  make(map[string][]byte),
		publicKeys: make(map[string][]gliderssh.PublicKey),
	}
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
	if len(config.passwords) == 0 && len(config.publicKeys) == 0 {
		return nil, invalidSSHConfig("at least one client credential is required")
	}
	if len(config.targets) == 0 {
		return nil, invalidSSHConfig("at least one forwarding target is required")
	}

	mux := &forwardingMux{targets: config.targets}
	server := &gliderssh.Server{
		HostSigners: config.hostKeys,
		PasswordHandler: func(ctx gliderssh.Context, password string) bool {
			expected, ok := config.passwords[ctx.User()]
			return ok && subtle.ConstantTimeCompare(expected, []byte(password)) == 1
		},
		PublicKeyHandler: func(ctx gliderssh.Context, key gliderssh.PublicKey) bool {
			for _, authorized := range config.publicKeys[ctx.User()] {
				if gliderssh.KeysEqual(key, authorized) {
					return true
				}
			}
			return false
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

func (m *forwardingMux) allowLocalTarget(_ gliderssh.Context, host string, port uint32) bool {
	if port == 0 || port > 0xFFFF {
		return false
	}
	target := forwardTarget{host: normalizeForwardHost(host), port: uint16(port)}
	_, ok := m.targets[target]
	return ok
}

func parseForwardTarget(value string) (forwardTarget, error) {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return forwardTarget{}, fmt.Errorf("target must be a host:port pair: %w", err)
	}
	if host == "" || strings.TrimSpace(host) != host {
		return forwardTarget{}, errors.New("target host must be non-empty and contain no surrounding whitespace")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return forwardTarget{}, errors.New("target port must be in the range 1-65535")
	}
	return forwardTarget{host: normalizeForwardHost(host), port: uint16(port)}, nil
}

func normalizeForwardHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

func invalidSSHConfig(message string) error {
	return errorx.EnsureStackTrace(fmt.Errorf("%w: %s", ErrInvalidSSHServerConfig, message))
}
