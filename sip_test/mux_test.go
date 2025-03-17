package sip_test

import (
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func Test_MuxPing(t *testing.T) {
	mux := sip.Mux(SimpleSyncClient{})
	err := mux.Ping()
	assert.NoError(t, err)
}

func Test_MuxReadEverything_Same(t *testing.T) {
	mux := sip.Mux(SimpleSyncClient{})
	done := make(chan *sip.ReadEverythingResponse, 2)
	go func() {
		response, err := mux.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		assert.Equal(t, sip.ReadEverythingResponse{}, response)
		done <- &response
	}()
	go func() {
		response, err := mux.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		assert.Equal(t, sip.ReadEverythingResponse{}, response)
		done <- &response
	}()

	r1 := <-done
	r2 := <-done
	assert.Equal(t, r1, r2)
}

func Test_MuxReadEverything_Different(t *testing.T) {
	mux := sip.Mux(SimpleSyncClient{})
	done := make(chan *sip.ReadEverythingResponse, 2)
	go func() {
		response, err := mux.ReadEverything(0, 0, 0)
		assert.NoError(t, err)
		assert.Equal(t, sip.ReadEverythingResponse{}, response)
		done <- &response
	}()
	go func() {
		response, err := mux.ReadEverything(0, 0, 1)
		assert.NoError(t, err)
		assert.Equal(t, sip.ReadEverythingResponse{}, response)
		done <- &response
	}()

	r1 := <-done
	r2 := <-done
	assert.Equal(t, r1, r2)
}
