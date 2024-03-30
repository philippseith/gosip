package sip_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestSmoke(t *testing.T) {
	l := NewListener()
	source := SimpleSyncClient{}
	ctx, cancel := context.WithCancel(context.Background())
	sip.Serve(ctx, l, source, sip.WithBusyTimeout(2001), sip.WithLeaseTimeout(10001))

	sipConn, err := sip.Dial("", "", sip.WithDial(func(network, address string) (io.ReadWriteCloser, error) {
		return l.Dial(network, address)
	}))
	assert.NoError(t, err)
	assert.Equal(t, 2001*time.Millisecond, sipConn.BusyTimeout())
	assert.Equal(t, 10001*time.Millisecond, sipConn.LeaseTimeout())

	resp, err := sipConn.ReadDataState(ctx, 0, 0, 0)
	assert.NoError(t, err)
	assert.Equal(t, sip.ReadDataStateResponse{}, resp)

	sipConn.Close()
	cancel()
	l.Close()
}
