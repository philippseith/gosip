package ext_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/philippseith/gosip/ext"
	"github.com/stretchr/testify/require"
	cryptossh "golang.org/x/crypto/ssh"
)

func TestSSHServerOnlyForwardsAuthenticatedAllowedTargets(t *testing.T) {
	targetAddr := startEchoTarget(t)

	hostSigner := generateSigner(t)
	clientSigner := generateSigner(t)
	server, err := ext.NewSSHServer(
		ext.WithHostSigner(hostSigner),
		ext.WithPasswordCredential("password-user", "correct horse battery staple"),
		ext.WithAuthorizedKey("key-user", cryptossh.MarshalAuthorizedKey(clientSigner.PublicKey())),
		ext.WithForwardingTargets(targetAddr),
	)
	require.NoError(t, err)

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

	passwordClient := dialSSH(t, listener.Addr().String(), "password-user", []cryptossh.AuthMethod{
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

	keyClient := dialSSH(t, listener.Addr().String(), "key-user", []cryptossh.AuthMethod{
		cryptossh.PublicKeys(clientSigner),
	}, hostSigner.PublicKey())
	verifyForward(t, keyClient, targetAddr)

	_, err = cryptossh.Dial("tcp", listener.Addr().String(), clientConfig(
		"password-user", []cryptossh.AuthMethod{cryptossh.Password("wrong password")}, hostSigner.PublicKey()))
	require.Error(t, err, "invalid password must not authenticate")

	unauthorizedKey := generateSigner(t)
	_, err = cryptossh.Dial("tcp", listener.Addr().String(), clientConfig(
		"key-user", []cryptossh.AuthMethod{cryptossh.PublicKeys(unauthorizedKey)}, hostSigner.PublicKey()))
	require.Error(t, err, "unlisted public key must not authenticate")
}

func TestNewSSHServerRequiresHostKeyCredentialAndTarget(t *testing.T) {
	hostSigner := generateSigner(t)

	_, err := ext.NewSSHServer(ext.WithHostSigner(hostSigner))
	require.ErrorIs(t, err, ext.ErrInvalidSSHServerConfig)

	_, err = ext.NewSSHServer(
		ext.WithHostSigner(hostSigner),
		ext.WithPasswordCredential("user", "password"),
	)
	require.ErrorIs(t, err, ext.ErrInvalidSSHServerConfig)

	_, err = ext.NewSSHServer(
		ext.WithHostSigner(hostSigner),
		ext.WithPasswordCredential("user", "password"),
		ext.WithForwardingTargets("not-a-host-port"),
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
