package sip_test

import (
	"net"
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestReadEverything(t *testing.T) {
	conn, err := net.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	_, err = sip.Connect(conn, 3000, 10000)
	assert.NoError(t, err)

	resp, err := sip.ReadEverything(conn, 0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, uint32(2), resp.DataLength)
}

func TestReadOnlyData(t *testing.T) {
	conn, err := net.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	_, err = sip.Connect(conn, 3000, 10000)
	assert.NoError(t, err)

	resp, err := sip.ReadOnlyData(conn, 0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, uint32(2), resp.DataLength)
}

func TestReadDescription(t *testing.T) {
	conn, err := net.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	_, err = sip.Connect(conn, 3000, 10000)
	assert.NoError(t, err)

	resp, err := sip.ReadDescription(conn, 0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, "us", resp.Unit)
}

func TestReadDataState(t *testing.T) {
	conn, err := net.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	_, err = sip.Connect(conn, 3000, 10000)
	assert.NoError(t, err)

	_, err = sip.ReadDataState(conn, 0, 0, 1)

	assert.NoError(t, err)
}
