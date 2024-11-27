package sip_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/philippseith/gosip/sip"
)

func TestReadEverything(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress)

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	resp, err := conn.ReadEverything(context.Background(), 0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, uint32(2), resp.DataLength)
}

func TestReadOnlyData(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress)

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	resp, err := conn.ReadOnlyData(context.Background(), 0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, uint32(2), resp.DataLength)
}

func TestReadDescription(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress)

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	resp, err := conn.ReadDescription(context.Background(), 0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, []byte("us"), resp.Unit)
}

func TestReadDataState(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress)

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	_, err = conn.ReadDataState(context.Background(), 0, 0, 1)

	assert.NoError(t, err)
}

func BenchmarkReadParallel(t *testing.B) {
	log.SetFlags(log.Lmicroseconds)

	conn, err := sip.Dial("tcp", serverAddress)
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	d1 := make(chan []byte)
	go func() {
		resp, err := conn.ReadOnlyData(context.Background(), 0, 0, 17)

		assert.NoError(t, err)

		d1 <- resp.Data
	}()
	d2 := make(chan []byte)
	go func() {
		resp, err := conn.ReadOnlyData(context.Background(), 0, 0, 17)

		assert.NoError(t, err)
		d2 <- resp.Data
	}()
	b1 := <-d1
	b2 := <-d2
	assert.Equal(t, b1, b2)
}

func TestReadS192(t *testing.T) {
	conn, err := sip.Dial("tcp", serverAddress, sip.WithConcurrentTransactionLimit(1))

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	defer measureTime("")()

	resp, err := conn.ReadOnlyData(context.Background(), 0, 0, 1789)

	assert.NoError(t, err)
	assert.NotEqual(t, 0, resp.DataLength)

	idns := make([]uint32, resp.DataLength/4)
	assert.NoError(t, binary.Read(bytes.NewReader(resp.Data), binary.LittleEndian, idns))

	log.Print("start")
	var wg sync.WaitGroup
	wg.Add(len(idns))
	for _, i := range idns {
		idn := i
		go func() {
			_, err := conn.ReadOnlyData(context.Background(), 0, 0, idn)
			assert.NoError(t, err)
			wg.Done()
		}()
	}
	wg.Wait()
	log.Print("stop")
}

func measureTime(id string) func() {
	now := time.Now()
	return func() {
		log.Print(id, " ", time.Now().Sub(now).Milliseconds())
	}
}

func TestChanStruct(t *testing.T) {
	ch := make(chan struct{}, 10000000)
	ch <- struct{}{}
}
