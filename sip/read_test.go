package sip_test

import (
	"bytes"
	"encoding/binary"
	"log"
	"sync"
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestReadEverything(t *testing.T) {
	conn, err := sip.Dial("tcp", address)

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	result := <-conn.ReadEverything(0, 0, 1)

	assert.NoError(t, result.Err)
	assert.Equal(t, uint32(2), result.Ok.DataLength)
}

func TestReadOnlyData(t *testing.T) {
	conn, err := sip.Dial("tcp", address)

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	result := <-conn.ReadOnlyData(0, 0, 1)

	assert.NoError(t, result.Err)
	assert.Equal(t, uint32(2), result.Ok.DataLength)
}

func TestReadDescription(t *testing.T) {
	conn, err := sip.Dial("tcp", address)

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	result := <-conn.ReadDescription(0, 0, 1)

	assert.NoError(t, result.Err)
	assert.Equal(t, []byte("us"), result.Ok.Unit)
}

func TestReadDataState(t *testing.T) {
	conn, err := sip.Dial("tcp", address)

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	result := <-conn.ReadDataState(0, 0, 1)

	assert.NoError(t, result.Err)
}

func BenchmarkReadParallel(t *testing.B) {
	log.SetFlags(log.Lmicroseconds)

	conn, err := sip.Dial("tcp", address)
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	d1 := make(chan []byte)
	go func() {
		result := <-conn.ReadOnlyData(0, 0, 17)

		assert.NoError(t, result.Err)

		d1 <- result.Ok.Data
	}()
	d2 := make(chan []byte)
	go func() {
		result := <-conn.ReadOnlyData(0, 0, 17)

		assert.NoError(t, result.Err)
		d2 <- result.Ok.Data
	}()
	b1 := <-d1
	b2 := <-d2
	assert.Equal(t, b1, b2)
}

func TestReadS192(t *testing.T) {
	conn, err := sip.Dial("tcp", address, sip.WithConcurrentTransactions(1))

	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	result := <-conn.ReadOnlyData(0, 0, 192)

	assert.NoError(t, result.Err)
	assert.NotEqual(t, 0, result.Ok.DataLength)

	idns := make([]uint32, result.Ok.DataLength/4)
	assert.NoError(t, binary.Read(bytes.NewReader(result.Ok.Data), binary.LittleEndian, idns))

	log.Print("start")
	var wg sync.WaitGroup
	wg.Add(len(idns))
	for _, i := range idns {
		idn := i
		go func() {
			result := <-conn.ReadEverything(0, 0, idn)
			assert.NoError(t, result.Err)
			wg.Done()
		}()
	}
	wg.Wait()
	log.Print("stop")
}

func TestChanStruct(t *testing.T) {
	ch := make(chan struct{}, 10000000)
	ch <- struct{}{}
}
