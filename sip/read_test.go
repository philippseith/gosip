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
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)
	assert.NoError(t, err)

	resp, ex, err := conn.ReadEverything(0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, uint16(0), ex.CommomErrorCode)
	assert.Equal(t, uint32(2), resp.DataLength)
}

func TestReadOnlyData(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)
	assert.NoError(t, err)

	resp, ex, err := conn.ReadOnlyData(0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, uint16(0), ex.CommomErrorCode)
	assert.Equal(t, uint32(2), resp.DataLength)
}

func TestReadDescription(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)
	assert.NoError(t, err)

	resp, ex, err := conn.ReadDescription(0, 0, 1)

	assert.NoError(t, err)
	assert.Equal(t, uint16(0), ex.CommomErrorCode)
	assert.Equal(t, []byte("us"), resp.Unit)
}

func TestReadDataState(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	ex, err := conn.Connect(3000, 10000)
	assert.NoError(t, err)

	_, ex, err = conn.ReadDataState(0, 0, 1)

	assert.Equal(t, uint16(0), ex.CommomErrorCode)
	assert.NoError(t, err)
}

func BenchmarkReadParallel(t *testing.B) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	_, err = conn.Connect(3000, 10000)
	assert.NoError(t, err)

	d1 := make(chan []byte)
	go func() {
		resp, ex, err := conn.ReadOnlyData(0, 0, 17)

		assert.NoError(t, err)
		assert.Equal(t, uint16(0), ex.CommomErrorCode)

		d1 <- resp.Data
	}()
	d2 := make(chan []byte)
	go func() {
		resp, ex, err := conn.ReadOnlyData(0, 0, 17)

		assert.NoError(t, err)
		assert.Equal(t, uint16(0), ex.CommomErrorCode)
		d2 <- resp.Data
	}()
	b1 := <-d1
	b2 := <-d2
	assert.Equal(t, b1, b2)
}

func TestReadS192(t *testing.T) {
	conn, err := sip.Dial("tcp", "localhost:35021")
	defer func() { _ = conn.Close() }()

	assert.NoError(t, err)

	_, err = conn.Connect(3000, 10000)
	assert.NoError(t, err)

	resp, ex, err := conn.ReadOnlyData(0, 0, 192)

	assert.NoError(t, err)
	assert.Equal(t, uint16(0), ex.CommomErrorCode)
	assert.NotEqual(t, 0, resp.DataLength)

	idns := make([]uint32, resp.DataLength/4)
	assert.NoError(t, binary.Read(bytes.NewReader(resp.Data), binary.LittleEndian, idns))

	log.Print("start")
	var wg sync.WaitGroup
	wg.Add(len(idns))
	for _, i := range idns {
		idn := i
		go func() {
			_, ex, err = conn.ReadEverything(0, 0, idn)
			assert.NoError(t, err)
			assert.Equal(t, uint16(0), ex.CommomErrorCode)
			wg.Done()
		}()
	}
	wg.Wait()
	log.Print("stop")
}
