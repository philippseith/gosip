package sip_test

import (
	"errors"
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestExceptionIs(t *testing.T) {
	e := sip.Exception{}
	assert.True(t, errors.Is(e, sip.Error))
}
