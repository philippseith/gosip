package ssh_test

import (
	"context"
	"testing"
	"time"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/philippseith/gosip/sip"
	"github.com/philippseith/gosip/ssh"
	"github.com/stretchr/testify/require"
	cryptossh "golang.org/x/crypto/ssh"
)

// One SSH connection must not be able to open an unbounded number of S/IP
// channels: each costs five goroutines in this process (sip.Serve accept loop,
// connection handler, two io.Copy loops of the bridge, request discarder), and the
// 64 connection limit of sip.Serve is per channel.
func TestSSHChannelsPerConnectionAreBounded(t *testing.T) {
	address, hostKey := newUnauthenticatedServer(t, func(context.Context, ssh.TargetRequest) (sip.SyncClient, error) {
		return &countingSIPSource{}, nil
	})
	greedy := dialSSH(t, address, "u", nil, hostKey)
	var opened []cryptossh.Channel
	var rejection error
	for range 1000 {
		channel, err := openDirectTCPIP(greedy, "h", 1)
		if err != nil {
			rejection = err
			break
		}
		opened = append(opened, channel)
	}
	t.Logf("one SSH connection opened %d channels, %d goroutines run gosip server code; rejection: %v", len(opened), gosipGoroutines(), rejection)
	require.LessOrEqual(t, len(opened), 64, "channels per SSH connection are unbounded")

	// The limit is per SSH connection: another connection is unaffected.
	other := dialSSH(t, address, "u", nil, hostKey)
	channel, err := openDirectTCPIP(other, "h", 1)
	require.NoError(t, err)
	_ = channel.Close()

	// Closing channels frees capacity again.
	for _, c := range opened {
		_ = c.Close()
	}
	require.Eventually(t, func() bool {
		c, err := openDirectTCPIP(greedy, "h", 1)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 3*time.Second, 20*time.Millisecond, "capacity is not released after channels close")
}

func TestSSHMaxChannelsPerConnectionOption(t *testing.T) {
	hostSigner := generateSigner(t)
	server := &gliderssh.Server{HostSigners: []gliderssh.Signer{hostSigner}}
	require.NoError(t, ssh.AttachSIPSource(context.Background(), server, func(context.Context, ssh.TargetRequest) (sip.SyncClient, error) {
		return &countingSIPSource{}, nil
	}, nil, ssh.WithMaxChannelsPerConnection(2)))
	client := dialSSH(t, startSSHServer(t, server), "u", nil, hostSigner.PublicKey())

	first, err := openDirectTCPIP(client, "h", 1)
	require.NoError(t, err)
	_, err = openDirectTCPIP(client, "h", 1)
	require.NoError(t, err)
	_, err = openDirectTCPIP(client, "h", 1)
	var openErr *cryptossh.OpenChannelError
	require.ErrorAs(t, err, &openErr)
	require.Equal(t, cryptossh.ResourceShortage, openErr.Reason)

	require.NoError(t, first.Close())
	require.Eventually(t, func() bool {
		c, err := openDirectTCPIP(client, "h", 1)
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	}, 3*time.Second, 20*time.Millisecond)
}

func TestAttachSIPSourceRejectsInvalidMaxChannels(t *testing.T) {
	server := &gliderssh.Server{}
	err := ssh.AttachSIPSource(context.Background(), server, func(context.Context, ssh.TargetRequest) (sip.SyncClient, error) {
		return nil, nil
	}, nil, ssh.WithMaxChannelsPerConnection(0))
	require.ErrorIs(t, err, ssh.ErrInvalidSIPMuxInstall)
}
