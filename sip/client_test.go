package sip_test

import (
	"encoding/binary"
	"log"
	"sync"
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

func TestBackupNoCork(t *testing.T) {
	c := sip.NewClient("tcp", serverAddress)

	var wg sync.WaitGroup
	defer func() {
		_ = c.Close()
	}()

	s192, _ := c.ReadOnlyData(0, 0, 192)
	idns := make([]uint32, len(s192.Data)/4)
	for i := 0; i < len(idns); i++ {
		idns[i] = binary.LittleEndian.Uint32(s192.Data[i*4 : (i+1)*4])
	}

	// This loop may cause TCP ZeroWindows from the drive
	for _, idn := range idns {
		cIdn := idn
		wg.Add(1)
		go func() {
			// log.Printf("%s", sip.Idn(cIdn))
			_, err := c.ReadEverything(0, 0, cIdn)
			if err != nil {
				log.Printf("Error: %v", err)
			}
			//log.Printf("RECV %s", sip.Idn(cIdn))
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestBackupCork(t *testing.T) {
	c := sip.NewClient("tcp", serverAddress, sip.WithCorking(2*time.Millisecond))

	var wg sync.WaitGroup
	defer func() {
		_ = c.Close()
	}()

	s192, _ := c.ReadOnlyData(0, 0, 192)
	idns := make([]uint32, len(s192.Data)/4)
	for i := 0; i < len(idns); i++ {
		idns[i] = binary.LittleEndian.Uint32(s192.Data[i*4 : (i+1)*4])
	}

	// This loop may cause TCP ZeroWindows from the drive
	for _, idn := range idns {
		cIdn := idn
		wg.Add(1)
		go func() {
			// log.Printf("%s", sip.Idn(cIdn))
			_, err := c.ReadEverything(0, 0, cIdn)
			if err != nil {
				log.Printf("Error: %v", err)
			}
			//log.Printf("RECV %s", sip.Idn(cIdn))
			wg.Done()
		}()
	}
	wg.Wait()
}
