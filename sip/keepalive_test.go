package sip_test

import (
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestNoKeepAlive(t *testing.T) {
	conn, err := sip.Dial("tcp", address)
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	err = conn.Ping()
	assert.NoError(t, err)

	// let the LeaseTimeout elapse
	<-time.After(conn.LeaseTimeout() + 100*time.Millisecond)

	// The connection should be closed now and Ping should err
	err = conn.Ping()
	assert.Error(t, err)
}

func TestKeepAlive(t *testing.T) {
	conn, err := sip.Dial("tcp", address, sip.WithSendKeepAlive())
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	err = conn.Ping()
	assert.NoError(t, err)

	// let the LeaseTimeout elapse
	<-time.After(conn.LeaseTimeout() + 100*time.Millisecond)

	// The connection should be held open by the KeepAlive Pin
	err = conn.Ping()
	assert.NoError(t, err)
}
