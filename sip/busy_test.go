package sip_test

import (
	"log"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestBusy(t *testing.T) {
	conn, err := sip.Dial("tcp", address)
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	log.Print("Ping1")
	err = conn.Ping()
	assert.NoError(t, err)

	// let the BusyTimeout elapse
	<-time.After(conn.BusyTimeout() + 100*time.Millisecond)

	// The connection should be closed now and Ping should err
	log.Print("Ping2")
	err = conn.Ping()
	assert.Error(t, err)

}
