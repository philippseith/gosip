package sip

import (
	"bytes"
	"context"
	"log"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func _TestUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "192.168.2.81:35021")

	assert.NoError(t, err)

	defer pc.Close()

	addr, err := net.ResolveUDPAddr("udp4", "255.255.255.255:35021")

	assert.NoError(t, err)

	writer := bytes.NewBuffer(make([]byte, 0, 17))
	br := BrowseRequest{
		IPAddress:          [4]byte(net.ParseIP("127.128.129.130").To4()),
		MasterOnly:         false,
		LowerSercosAddress: 0,
		UpperSercosAddress: 511,
	}
	hdr := Header{
		TransactionID: 1,
		MessageType:   br.MessageType(),
	}
	hdr.Write(writer)
	br.Write(writer)
	pc.WriteTo(writer.Bytes(), addr)
}

func TestDecode(t *testing.T) {
	b := []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x7f, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x04}
	reader := bytes.NewBuffer(b)
	br := BrowseRequest{}
	br.Read(reader)
	log.Print(br)
}

func _TestP1354(t *testing.T) {
	conn, err := Dial("tcp", "192.168.112.14:35021", WithBusyTimeout(5000))
	assert.NoError(t, err)

	resp, err := conn.ReadOnlyData(context.Background(), 0, 0, 0x8000+1354)
	assert.NoError(t, err)

	err = conn.WriteData(context.Background(), 0, 0, 0x8000+1354, resp.Data)
}
