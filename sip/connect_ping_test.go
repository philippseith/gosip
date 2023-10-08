package sip_test

import (
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

var address = "127.0.0.1:35021"

func TestConnect(t *testing.T) {
	conn, err := sip.Dial("tcp", address)
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)
	assert.NotEmpty(t, conn.MessageTypes())
}

func TestConnectNoServer(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35022")
	defer func() { _ = conn.Close() }()

	assert.Error(t, err)
}

func TestConnectTimeout(t *testing.T) {
	conn, err := sip.Dial("tcp", address, sip.BusyTimeout(1))

	assert.Nil(t, conn)
	assert.Error(t, err)
}

func TestPing(t *testing.T) {
	conn, err := sip.Dial("tcp", address)
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	err = conn.Ping()

	assert.NoError(t, err)
}

func TestPingShortClosedConnection(t *testing.T) {
	conn, err := sip.Dial("tcp", address)

	assert.NotNil(t, conn)
	assert.NoError(t, err)

	assert.NoError(t, conn.Close())

	err = conn.Ping()

	assert.Equal(t, sip.ErrorClosed, err)
}
