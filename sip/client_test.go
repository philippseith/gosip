package sip_test

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/philippseith/gosip/sip"
)

func TestClientReconnectLeaseExceeded(t *testing.T) {
	c := sip.NewClient("tcp", serverAddress, sip.WithCorking(60*time.Millisecond))
	assert.NoError(t, c.Ping())
	assert.NoError(t, c.Ping())
	log.Printf("Waiting %v", c.LeaseTimeout())
	<-time.After(c.LeaseTimeout() + 500*time.Millisecond)
	assert.NoError(t, c.Ping())
}

func TestStress(t *testing.T) {
	c := sip.NewClient("tcp", serverAddress, sip.WithCorking(60*time.Millisecond))
	// the sequential version should be no stress at all
	// c := sip.NewClient("tcp", serverAddress, sip.WithConcurrentTransactionLimit(1))
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		_ = c.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	tick := time.NewTicker(time.Millisecond * 5)
	count := int32(0)

	// This loop may cause TCP ZeroWindows from the drive
	for {
		select {
		case <-tick.C:
			go func() {
				select {
				case <-ctx.Done():
					return
				default:
					wg.Add(1)
					_, err := c.ReadOnlyData(0, 0, 1)
					if err != nil {
						log.Printf("Error: %v", err)
					}
					wg.Done()
					atomic.AddInt32(&count, 1)
				}
			}()
		case <-ctx.Done():
			log.Printf("count: %v", count)
			return
		}
	}
}
