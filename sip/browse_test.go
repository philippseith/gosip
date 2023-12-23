package sip_test

import (
	"context"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

var interfaceIP = "192.168.2.81"

func TestBrowse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := sip.Browse(ctx, interfaceIP)
	assert.NoError(t, err)

	var resps []sip.BrowseResponse
	for resp := range ch {
		assert.NoError(t, resp.Err)
		if resp.Err == nil {
			resps = append(resps, resp.Ok)
		}
	}
	assert.NotEmpty(t, resps)
}
