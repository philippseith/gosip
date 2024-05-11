package sip_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/philippseith/gosip/sip"
)

func TestNoKeepAlive(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress)
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	err = conn.Ping(context.Background())
	assert.NoError(t, err)

	// let the LeaseTimeout elapse
	<-time.After(conn.LeaseTimeout() + 100*time.Millisecond)

	// The connection should be closed now and Ping should err
	err = conn.Ping(context.Background())
	assert.Error(t, err)
}

func TestKeepAlive(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress, sip.WithSendKeepAlive())
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	err = conn.Ping(context.Background())
	assert.NoError(t, err)

	// let the LeaseTimeout elapse
	<-time.After(conn.LeaseTimeout() + 100*time.Millisecond)

	// The connection should be held open by the KeepAlive Pin
	err = conn.Ping(context.Background())
	assert.NoError(t, err)
}
