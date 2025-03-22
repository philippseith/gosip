package sip_test

import (
	"context"
	"net"
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func Test_MuxPing(t *testing.T) {
	mux := sip.NewMux(SimpleSyncClient{})
	err := mux.Ping()
	assert.NoError(t, err)
}

func Test_MuxReadEverything_Same(t *testing.T) {
	mux := sip.NewMux(SimpleSyncClient{})
	done := make(chan *sip.ReadEverythingResponse, 2)
	go func() {
		response, err := mux.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		done <- &response
	}()
	go func() {
		response, err := mux.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		done <- &response
	}()

	r1 := <-done
	r2 := <-done
	assert.Equal(t, r1, r2)
}

func Test_MuxReadEverything_Different(t *testing.T) {
	mux := sip.NewMux(SimpleSyncClient{})
	done := make(chan *sip.ReadEverythingResponse, 2)
	go func() {
		response, err := mux.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		done <- &response
	}()
	go func() {
		response, err := mux.ReadEverything(0, 0, 1)
		assert.NoError(t, err)
		done <- &response
	}()

	r1 := <-done
	r2 := <-done
	assert.NotEqual(t, r1, r2)
}

func TestMuxServe(t *testing.T) {
	ctx := context.Background()
	defer ctx.Done()

	mux := sip.NewMux(SimpleSyncClient{})

	listener, err := net.Listen("tcp", "127.0.0.1:8086")
	assert.NoError(t, err)
	defer func() {
		if listener != nil {
			listener.Close()
		}
	}()

	go sip.Serve(ctx, listener, mux)

	assert.NoError(t, err)
	clientA := sip.NewClient("tcp", "127.0.0.1:8086")
	defer clientA.Close()
	clientB := sip.NewClient("tcp", "127.0.0.1:8086")
	defer clientB.Close()

	assert.NoError(t, clientA.Ping())
	assert.NoError(t, clientB.Ping())

	done := make(chan *sip.ReadEverythingResponse, 2)
	go func() {
		response, err := clientA.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		done <- &response
	}()
	go func() {
		response, err := clientB.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		done <- &response
	}()

	r1 := <-done
	r2 := <-done
	assert.Equal(t, r1, r2)

	go func() {
		response, err := clientA.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		done <- &response
	}()
	go func() {
		response, err := clientB.ReadEverything(0, 0, 1)
		assert.NoError(t, err)
		done <- &response
	}()

	r1 = <-done
	r2 = <-done
	assert.Equal(t, r1, r2)
}
