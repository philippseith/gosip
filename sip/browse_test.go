package sip_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/philippseith/gosip/sip"
)

func TestBrowse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := sip.Browse(ctx, interfaceName)
	assert.NoError(t, err)

	var resps []*sip.BrowseResponse
	for resp := range ch {
		assert.NoError(t, resp.Err)
		if resp.Err == nil {
			resps = append(resps, resp.Ok)
		}
	}
	assert.NotEmpty(t, resps)
}
