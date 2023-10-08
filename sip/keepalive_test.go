package sip_test

import (
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/philippseith/gosip/sip"
)

func TestNoKeepAlive(t *testing.T) {
	log.SetFlags(log.Lmicroseconds)

	conn, err := sip.Dial("tcp", address)
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	err = conn.Ping()
	assert.NoError(t, err)

	// let the LeaseTimeout elapse
	<-time.After(conn.LeaseTimeout() + 100*time.Millisecond)

	// The connection should be closed now and Ping should err
	log.Print("2nd Ping")
	err = conn.Ping()
	assert.Error(t, err)
	log.Print("2nd Ping done")
	// <-time.After(10 * time.Millisecond)
	log.Print("3rd Ping")
	err = conn.Ping()
	assert.NoError(t, err)
	log.Print("3rd Ping done")
	<-time.After(10 * time.Millisecond)
	log.Print("End")
}

func TestKeepAlive(t *testing.T) {
	conn, err := sip.Dial("tcp", address, sip.SendKeepAlive())
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
