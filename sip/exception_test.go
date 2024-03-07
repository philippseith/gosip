package sip_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/assert"
)

func TestExceptionIs(t *testing.T) {
	e := sip.Exception{
		SpecificErrorCode: uint32(99),
		CommonErrorCode:   uint16(100),
	}
	assert.True(t, errors.Is(e, sip.Error))

	w := fmt.Errorf("wrapped: %w", e)
	var ex sip.Exception

	assert.True(t, errors.As(w, &ex))

	assert.Equal(t, e, ex)
}
