package ext_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/philippseith/gosip/ext"
	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/require"
	cryptossh "golang.org/x/crypto/ssh"
)

func TestSSHServerOnlyForwardsAuthenticatedAllowedTargets(t *testing.T) {
	targetAddr := startEchoTarget(t)
	targetHost, targetPortText, err := net.SplitHostPort(targetAddr)
	require.NoError(t, err)
	targetPort, err := strconv.ParseUint(targetPortText, 10, 16)
	require.NoError(t, err)

	hostSigner := generateSigner(t)
	clientSigner := generateSigner(t)
	server, err := ext.NewSSHServer(
		ext.WithHostSigner(hostSigner),
		ext.WithLoginCallback(func(_ context.Context, input ext.LoginInput) bool {
			switch input.Method {
			case ext.LoginPassword:
				return input.Username == "password-user" && input.Password == "correct horse battery staple"
			case ext.LoginPublicKey:
				return input.Username == "key-user" && gliderssh.KeysEqual(input.PublicKey, clientSigner.PublicKey())
			default:
				return false
			}
		}),
		ext.WithTargetCallback(func(_ context.Context, target ext.TargetRequest) bool {
			return (target.Username == "password-user" || target.Username == "key-user") &&
				target.Host == targetHost && target.Port == uint32(targetPort)
		}),
	)
	require.NoError(t, err)

	sshAddress := startSSHServer(t, server)

	passwordClient := dialSSH(t, sshAddress, "password-user", []cryptossh.AuthMethod{
		cryptossh.Password("correct horse battery staple"),
	}, hostSigner.PublicKey())
	verifyForward(t, passwordClient, targetAddr)

	_, err = passwordClient.Dial("tcp", "127.0.0.1:1")
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "port forwarding is disabled")

	_, err = passwordClient.NewSession()
	require.Error(t, err, "session channels must not be accepted")
	_, err = passwordClient.Listen("tcp", "127.0.0.1:0")
	require.Error(t, err, "remote forwarding requests must not be accepted")

	keyClient := dialSSH(t, sshAddress, "key-user", []cryptossh.AuthMethod{
		cryptossh.PublicKeys(clientSigner),
	}, hostSigner.PublicKey())
	verifyForward(t, keyClient, targetAddr)

	_, err = cryptossh.Dial("tcp", sshAddress, clientConfig(
		"password-user", []cryptossh.AuthMethod{cryptossh.Password("wrong password")}, hostSigner.PublicKey()))
	require.Error(t, err, "invalid password must not authenticate")

	unauthorizedKey := generateSigner(t)
	_, err = cryptossh.Dial("tcp", sshAddress, clientConfig(
		"key-user", []cryptossh.AuthMethod{cryptossh.PublicKeys(unauthorizedKey)}, hostSigner.PublicKey()))
	require.Error(t, err, "unlisted public key must not authenticate")
}

func TestNewSSHServerRequiresHostKeyAndCallbacks(t *testing.T) {
	hostSigner := generateSigner(t)
	loginCheck := ext.LoginCallback(func(context.Context, ext.LoginInput) bool { return false })
	targetCheck := ext.TargetCallback(func(context.Context, ext.TargetRequest) bool { return false })

	_, err := ext.NewSSHServer(ext.WithHostSigner(hostSigner))
	require.ErrorIs(t, err, ext.ErrInvalidSSHServerConfig)
	_, err = ext.NewSSHServer(ext.WithHostSigner(hostSigner), ext.WithLoginCallback(loginCheck))
	require.ErrorIs(t, err, ext.ErrInvalidSSHServerConfig)
	_, err = ext.NewSSHServer(ext.WithHostSigner(hostSigner), ext.WithTargetCallback(targetCheck))
	require.ErrorIs(t, err, ext.ErrInvalidSSHServerConfig)
	_, err = ext.NewSSHServer(
		ext.WithHostSigner(hostSigner),
		ext.WithLoginCallback(nil),
		ext.WithTargetCallback(targetCheck),
	)
	require.ErrorIs(t, err, ext.ErrInvalidSSHServerConfig)
	_, err = ext.NewSSHServer(
		ext.WithHostSigner(hostSigner),
		ext.WithLoginCallback(loginCheck),
		ext.WithTargetCallback(nil),
	)
	require.ErrorIs(t, err, ext.ErrInvalidSSHServerConfig)
}

func startEchoTarget(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("echo target did not stop")
		}
	})
	return listener.Addr().String()
}

func generateSigner(t *testing.T) cryptossh.Signer {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := cryptossh.NewSignerFromKey(privateKey)
	require.NoError(t, err)
	return signer
}

func dialSSH(t *testing.T, address, user string, auth []cryptossh.AuthMethod, hostKey cryptossh.PublicKey) *cryptossh.Client {
	t.Helper()
	client, err := cryptossh.Dial("tcp", address, clientConfig(user, auth, hostKey))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func clientConfig(user string, auth []cryptossh.AuthMethod, hostKey cryptossh.PublicKey) *cryptossh.ClientConfig {
	return &cryptossh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: cryptossh.FixedHostKey(hostKey),
		Timeout:         2 * time.Second,
	}
}

func verifyForward(t *testing.T, client *cryptossh.Client, target string) {
	t.Helper()
	conn, err := client.Dial("tcp", target)
	require.NoError(t, err)
	defer conn.Close()
	payload := []byte("forwarded over real SSH")
	type echoResult struct {
		response []byte
		err      error
	}
	done := make(chan echoResult, 1)
	go func() {
		if _, err := conn.Write(payload); err != nil {
			done <- echoResult{err: err}
			return
		}
		response := make([]byte, len(payload))
		_, err := io.ReadFull(conn, response)
		done <- echoResult{response: response, err: err}
	}()
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, payload, result.response)
	case <-time.After(2 * time.Second):
		_ = conn.Close()
		t.Fatal("forwarded echo timed out")
	}
}

func TestMultipleSIPClientsReachOneMuxOverSSH(t *testing.T) {
	source := &countingSIPSource{}
	sipTarget := startSIPMuxTarget(t, source)
	targetHost, targetPortText, err := net.SplitHostPort(sipTarget)
	require.NoError(t, err)
	targetPort, err := strconv.ParseUint(targetPortText, 10, 16)
	require.NoError(t, err)

	hostSigner := generateSigner(t)
	server, err := ext.NewSSHServer(
		ext.WithHostSigner(hostSigner),
		ext.WithLoginCallback(func(_ context.Context, input ext.LoginInput) bool {
			return input.Method == ext.LoginPassword && input.Username == "operator" && input.Password == "secret"
		}),
		ext.WithTargetCallback(func(_ context.Context, target ext.TargetRequest) bool {
			return target.Username == "operator" && target.Host == targetHost && target.Port == uint32(targetPort)
		}),
	)
	require.NoError(t, err)
	sshAddress := startSSHServer(t, server)

	sshClientA := dialSSH(t, sshAddress, "operator", []cryptossh.AuthMethod{cryptossh.Password("secret")}, hostSigner.PublicKey())
	sshClientB := dialSSH(t, sshAddress, "operator", []cryptossh.AuthMethod{cryptossh.Password("secret")}, hostSigner.PublicKey())
	sipClientA := newSIPClientOverSSH(t, sshClientA, sipTarget)
	sipClientB := newSIPClientOverSSH(t, sshClientB, sipTarget)

	require.NoError(t, sipClientA.Ping())
	require.NoError(t, sipClientB.Ping())
	stateA, err := sipClientA.ReadDataState(0, 0, 0x101)
	require.NoError(t, err)
	stateB, err := sipClientB.ReadDataState(0, 0, 0x202)
	require.NoError(t, err)
	require.Equal(t, uint16(0x101), stateA.DataState)
	require.Equal(t, uint16(0x202), stateB.DataState)
	require.EqualValues(t, 2, source.reads.Load())
}

func startSSHServer(t *testing.T, server *ext.SSHServer) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		select {
		case err := <-serveDone:
			if !errors.Is(err, gliderssh.ErrServerClosed) {
				t.Errorf("Serve returned unexpected error: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("SSH server did not stop")
		}
	})
	return listener.Addr().String()
}

func startSIPMuxTarget(t *testing.T, source sip.SyncClient) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, sip.Serve(ctx, listener, sip.NewMux(source)))
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
	})
	return listener.Addr().String()
}

func newSIPClientOverSSH(t *testing.T, client *cryptossh.Client, target string) sip.Client {
	t.Helper()
	sipClient, err := sip.NewClient("tcp", target, sip.WithDial(func(_ context.Context, network, address string) (io.ReadWriteCloser, error) {
		return client.Dial(network, address)
	}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sipClient.Close() })
	return sipClient
}

type countingSIPSource struct {
	reads atomic.Int32
}

func (*countingSIPSource) Ping(...sip.RequestOption) error { return nil }

func (*countingSIPSource) ReadEverything(int, int, uint32, ...sip.RequestOption) (sip.ReadEverythingResponse, error) {
	return sip.ReadEverythingResponse{}, nil
}

func (*countingSIPSource) ReadOnlyData(int, int, uint32, ...sip.RequestOption) (sip.ReadOnlyDataResponse, error) {
	return sip.ReadOnlyDataResponse{}, nil
}

func (*countingSIPSource) ReadDescription(int, int, uint32, ...sip.RequestOption) (sip.ReadDescriptionResponse, error) {
	return sip.ReadDescriptionResponse{}, nil
}

func (s *countingSIPSource) ReadDataState(_, _ int, idn uint32, _ ...sip.RequestOption) (sip.ReadDataStateResponse, error) {
	s.reads.Add(1)
	return sip.ReadDataStateResponse{DataState: uint16(idn)}, nil
}

func (*countingSIPSource) WriteData(int, int, uint32, []byte, ...sip.RequestOption) error {
	return nil
}
