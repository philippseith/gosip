package sip_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestSetIP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := sip.SetIP(ctx, interfaceName, setIPNode,
		net.IPv4(192, 168, 112, 113), net.IPv4zero, true)
	assert.NoError(t, err)

	var resps []*sip.SetIPResponse
	for resp := range ch {
		assert.NoError(t, resp.Err)
		if resp.Err == nil {
			resps = append(resps, resp.Ok)
		}
	}
	assert.NotEmpty(t, resps)
}
