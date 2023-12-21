package sip

import (
	"bytes"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp4", ":35021")

	assert.NoError(t, err)

	defer pc.Close()

	addr, err := net.ResolveUDPAddr("udp4", "255.255.255.255:35021")

	assert.NoError(t, err)

	writer := bytes.NewBuffer(make([]byte, 9))
	br := BrowseRequest{
		IPAddress:          [4]byte(net.ParseIP("127.0.0.1").To4()),
		MasterOnly:         false,
		LowerSercosAddress: 0,
		UpperSercosAddress: 1024,
	}
	br.Write(writer)
	pc.WriteTo(writer.Bytes(), addr)
}
