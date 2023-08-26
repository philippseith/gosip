package sip_test

import (
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestReadEverything(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)
	assert.NoError(t, err)

	resp, ex, err := conn.ReadEverything(0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, 0, ex.CommomErrorCode)
	assert.Equal(t, uint32(2), resp.DataLength)
}

func TestReadOnlyData(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)
	assert.NoError(t, err)

	resp, ex, err := conn.ReadOnlyData(0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, 0, ex.CommomErrorCode)
	assert.Equal(t, uint32(2), resp.DataLength)
}

func TestReadDescription(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)
	assert.NoError(t, err)

	resp, ex, err := conn.ReadDescription(0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, 0, ex.CommomErrorCode)
	assert.Equal(t, "us", resp.Unit)
}

func TestReadDataState(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)
	assert.NoError(t, err)

	_, ex, err = conn.ReadDataState(0, 0, 1)

	assert.Equal(t, 0, ex.CommomErrorCode)
	assert.NoError(t, err)
}
