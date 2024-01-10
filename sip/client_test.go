package sip_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestClientReconnectLeaseExceeded(t *testing.T) {
	c := sip.NewClient("tcp", serverAddress)
	assert.NoError(t, c.Ping())
	assert.NoError(t, c.Ping())
	log.Printf("Waiting %v", c.LeaseTimeout())
	<-time.After(c.LeaseTimeout() + 500*time.Millisecond)
	assert.NoError(t, c.Ping())
}

func TestStress(t *testing.T) {
	c := sip.NewClient("tcp", serverAddress)
	// the sequentialized version should be no stress at all
	// c := sip.NewClient("tcp", serverAddress, sip.WithConcurrentTransactionLimit(1))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	tick := time.NewTicker(time.Millisecond * 15)

	// This loop may cause TCP ZeroWindows from the drive
	for {
		select {
		case <-tick.C:
			go func() {
				_, err := c.ReadOnlyData(0, 0, 1)
				if err != nil {
					log.Printf("Error: %v", err)
				}
			}()
		case <-ctx.Done():
			return
		}

	}
}
