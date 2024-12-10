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
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	assert.NoError(t, err)
	if conn != nil {
		assert.NotEmpty(t, conn.MessageTypes())
	}
}

func TestConnectNoServer(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35022")
	defer func() {
		if conn != nil {
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()

	assert.Error(t, err)
}

func _TestConnectTimeout(t *testing.T) {
	// This does only work with relatively slow sip servers. IndraDrive is able to answer in less than 1ms :-)
	_, err := sip.Dial("tcp", serverAddress, sip.WithBusyTimeout(1))

	assert.Error(t, err)
}

func TestPing(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress)
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

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
