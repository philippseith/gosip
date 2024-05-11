package sip_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/philippseith/gosip/sip"
)

func TestConnect(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress)
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)
	assert.NotEmpty(t, conn.MessageTypes())
}

func TestConnectNoServer(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35022")
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	assert.Error(t, err)
}

func TestConnectTimeout(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress, sip.WithBusyTimeout(1))

	assert.Nil(t, conn)
	assert.Error(t, err)
}

func TestPing(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress)
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	err = conn.Ping(context.Background())

	assert.NoError(t, err)
}

func TestPingShortClosedConnection(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress)

	assert.NotNil(t, conn)
	assert.NoError(t, err)

	assert.NoError(t, conn.Close())

	err = conn.Ping(context.Background())

	assert.True(t, errors.Is(err, sip.ErrorClosed))
}
