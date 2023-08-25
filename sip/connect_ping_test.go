package sip_test

import (
	"net"
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestConnect(t *testing.T) {
	conn, err := net.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	resp, ex, err := sip.Connect(conn, 3000, 10000)

	assert.NoError(t, err)
	assert.Equal(t, 0, ex.CommomErrorCode)

	assert.NotEmpty(t, resp.MessageTypes)
}

func TestPing(t *testing.T) {
	conn, err := net.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	_, ex, err := sip.Connect(conn, 3000, 10000)
	assert.NoError(t, err)
	assert.Equal(t, 0, ex.CommomErrorCode)

	ex, err = sip.Ping(conn)

	assert.NoError(t, err)
	assert.Equal(t, 0, ex.CommomErrorCode)
}
