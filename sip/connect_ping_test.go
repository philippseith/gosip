package sip_test

import (
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestConnect(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)

	assert.NoError(t, err)
	assert.Equal(t, uint16(0), ex.CommomErrorCode)

	assert.NotEmpty(t, conn.MessageTypes())
}

func TestPing(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)
	assert.NoError(t, err)
	assert.Equal(t, uint16(0), ex.CommomErrorCode)

	ex, err = conn.Ping()

	assert.NoError(t, err)
	assert.Equal(t, uint16(0), ex.CommomErrorCode)
}
