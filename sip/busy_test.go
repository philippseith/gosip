package sip_test

import (
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestBusy(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	err = conn.Ping()
	assert.NoError(t, err)

	// let the BusyTimeout elapse
	<-time.After(conn.BusyTimeout() + 100*time.Millisecond)

	// The connection should be closed now and Ping should err
	err = conn.Ping()
	assert.Error(t, err)
	// TODO is this the wanted behavior?
}
