package sip_test

import (
	"context"
	"testing"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestIdentify(t *testing.T) {

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := sip.Identify(ctx, interfaceName, identifyNode)
	assert.NoError(t, err)

	var resps []*sip.IdentifyResponse
	for resp := range ch {
		assert.NoError(t, resp.Err)
		if resp.Err == nil {
			resps = append(resps, resp.Ok)
		}
	}
	assert.NotEmpty(t, resps)
}
