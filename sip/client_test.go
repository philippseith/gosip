package sip_test

import (
	"log"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestClientReconnectLeaseExceeded(t *testing.T) {
	c := sip.NewClient("tcp", address)
	assert.NoError(t, <-c.Ping())
	assert.NoError(t, <-c.Ping())
	log.Printf("Waiting %v", c.LeaseTimeout())
	<-time.After(c.LeaseTimeout() + 500*time.Millisecond)
	assert.NoError(t, <-c.Ping())
}
