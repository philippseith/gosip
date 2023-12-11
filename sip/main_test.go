package sip_test

import (
	"log"
	"testing"
)

func TestMain(m *testing.M) {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	m.Run()
}
