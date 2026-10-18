package ext_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand"
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

func TestGliderlabsSSHServerForwardsAuthenticatedAllowedTCPApplications(t *testing.T) {
	targetAddr := startEchoTarget(t)
	targetHost, targetPortText, err := net.SplitHostPort(targetAddr)
	require.NoError(t, err)
	targetPort, err := strconv.ParseUint(targetPortText, 10, 16)
	require.NoError(t, err)

	hostSigner := generateSigner(t)
	clientSigner := generateSigner(t)
	server := &gliderssh.Server{
		HostSigners: []gliderssh.Signer{hostSigner},
		PasswordHandler: func(ctx gliderssh.Context, password string) bool {
			return ctx.User() == "password-user" && password == "correct horse battery staple"
		},
		PublicKeyHandler: func(ctx gliderssh.Context, key gliderssh.PublicKey) bool {
			return ctx.User() == "key-user" && gliderssh.KeysEqual(key, clientSigner.PublicKey())
		},
		LocalPortForwardingCallback: func(ctx gliderssh.Context, host string, port uint32) bool {
			return (ctx.User() == "password-user" || ctx.User() == "key-user") &&
				host == targetHost && port == uint32(targetPort)
		},
		ReversePortForwardingCallback: func(gliderssh.Context, string, uint32) bool {
			return false
		},
		ChannelHandlers: map[string]gliderssh.ChannelHandler{
			"direct-tcpip": gliderssh.DirectTCPIPHandler,
		},
		RequestHandlers:   map[string]gliderssh.RequestHandler{},
		SubsystemHandlers: map[string]gliderssh.SubsystemHandler{},
	}
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

func TestAttachSIPSourceRequiresAuthentication(t *testing.T) {
	factory := func(context.Context, ext.TargetRequest) (sip.SyncClient, error) {
		return &countingSIPSource{}, nil
	}
	err := ext.AttachSIPSource(context.Background(), &gliderssh.Server{}, factory, nil)
	require.ErrorIs(t, err, ext.ErrInvalidSIPMuxInstall)
}

func TestAttachSIPSourceAppliesConnOptions(t *testing.T) {
	hostSigner := generateSigner(t)
	server := &gliderssh.Server{
		HostSigners: []gliderssh.Signer{hostSigner},
		PasswordHandler: func(ctx gliderssh.Context, password string) bool {
			return ctx.User() == "operator" && password == "secret"
		},
	}
	factory := func(context.Context, ext.TargetRequest) (sip.SyncClient, error) {
		return &countingSIPSource{}, nil
	}
	require.NoError(t, ext.AttachSIPSource(context.Background(), server, factory, []sip.ConnOption{sip.WithBusyTimeout(0)}))
	sshAddress := startSSHServer(t, server)
	client := dialSSH(t, sshAddress, "operator", []cryptossh.AuthMethod{cryptossh.Password("secret")}, hostSigner.PublicKey())
	_, err := client.Dial("tcp", "127.0.0.1:35021")
	require.Error(t, err, "invalid Serve options must reject the channel")
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

func TestConcurrentSIPClientsReachOneMuxOverSSH(t *testing.T) {
	const sipTarget = "127.0.0.1:35021"
	source := &countingSIPSource{}
	hostSigner := generateSigner(t)
	sharedMux := sip.NewMux(source)
	server := &gliderssh.Server{
		HostSigners: []gliderssh.Signer{hostSigner},
		PasswordHandler: func(ctx gliderssh.Context, password string) bool {
			return ctx.User() == "operator" && password == "secret"
		},
		ChannelHandlers: map[string]gliderssh.ChannelHandler{
			"session": func(_ *gliderssh.Server, _ *cryptossh.ServerConn, newChannel cryptossh.NewChannel, _ gliderssh.Context) {
				channel, requests, err := newChannel.Accept()
				if err != nil {
					return
				}
				go cryptossh.DiscardRequests(requests)
				_, _ = io.Copy(channel, channel)
				_ = channel.Close()
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	factory := ext.SIPSourceFactory(func(ctx context.Context, target ext.TargetRequest) (sip.SyncClient, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if target.Username != "operator" || target.Host != "127.0.0.1" || target.Port != 35021 {
			return nil, errors.New("forwarding target is not allowed")
		}
		return sharedMux, nil
	})
	err := ext.AttachSIPSource(ctx, server, factory, []sip.ConnOption{sip.WithBusyTimeout(3000)})
	require.NoError(t, err)
	sshAddress := startSSHServer(t, server)

	sshClientA := dialSSH(t, sshAddress, "operator", []cryptossh.AuthMethod{cryptossh.Password("secret")}, hostSigner.PublicKey())
	sshClientB := dialSSH(t, sshAddress, "operator", []cryptossh.AuthMethod{cryptossh.Password("secret")}, hostSigner.PublicKey())
	_, err = sshClientA.Dial("tcp", "127.0.0.1:35022")
	require.Error(t, err, "source factory must reject unauthorized destinations")
	sipClientA := newSIPClientOverSSH(t, sshClientA, sipTarget)
	sipClientB := newSIPClientOverSSH(t, sshClientB, sipTarget)
	sessionChannel, sessionRequests, err := sshClientA.OpenChannel("session", nil)
	require.NoError(t, err)
	go cryptossh.DiscardRequests(sessionRequests)
	sessionPayload := []byte("other SSH application")
	_, err = sessionChannel.Write(sessionPayload)
	require.NoError(t, err)
	sessionResponse := make([]byte, len(sessionPayload))
	_, err = io.ReadFull(sessionChannel, sessionResponse)
	require.NoError(t, err)
	require.Equal(t, sessionPayload, sessionResponse)
	_ = sessionChannel.Close()

	const requestsPerClient = 20
	start := make(chan struct{})
	results := make(chan error, 2)
	readStates := func(client sip.Client, firstIDN uint32) {
		<-start
		for i := range requestsPerClient {
			idn := firstIDN + uint32(i)
			response, err := client.ReadDataState(0, 0, idn)
			if err != nil {
				results <- fmt.Errorf("read IDN 0x%x: %w", idn, err)
				return
			}
			if response.DataState != uint16(idn) {
				results <- fmt.Errorf("read IDN 0x%x returned state 0x%x", idn, response.DataState)
				return
			}
		}
		results <- nil
	}
	go readStates(sipClientA, 0x1000)
	go readStates(sipClientB, 0x2000)
	close(start)
	timeout := time.After(10 * time.Second)
	for range 2 {
		select {
		case err := <-results:
			require.NoError(t, err)
		case <-timeout:
			t.Fatal("timed out waiting for concurrent SIP clients over SSH")
		}
	}
	require.EqualValues(t, 2*requestsPerClient, source.reads.Load())
}

func startSSHServer(t *testing.T, server *gliderssh.Server) string {
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
	time.Sleep(time.Duration(5+mathrand.Intn(11)) * time.Millisecond)
	s.reads.Add(1)
	return sip.ReadDataStateResponse{DataState: uint16(idn)}, nil
}

func (*countingSIPSource) WriteData(int, int, uint32, []byte, ...sip.RequestOption) error {
	return nil
}
