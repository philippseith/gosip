package sip

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func _TestUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "192.168.112.74:35021")

	assert.NoError(t, err)

	defer pc.Close()

	addr, err := net.ResolveUDPAddr("udp4", "255.255.255.255:35021")

	assert.NoError(t, err)

	writer := bytes.NewBuffer(make([]byte, 0, 17))
	br := BrowseRequest{
		IPAddress:          [4]byte(net.ParseIP("255.255.255.255").To4()),
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

func _TestBroadcast(t *testing.T) {

	broadcastAddr := "192.168.112.255:35021"
	localAddr := &net.UDPAddr{IP: net.ParseIP("192.168.112.74"), Port: 0}

	broadcastUDPAddr, err := net.ResolveUDPAddr("udp", broadcastAddr)
	// Listen on UDP port 35021 for all interfaces
	sender, err := net.DialUDP("udp", localAddr, broadcastUDPAddr)
	assert.NoError(t, err)

	writer := bytes.NewBuffer(make([]byte, 0, 17))
	br := BrowseRequest{
		IPAddress:          [4]byte(net.ParseIP("255.255.255.255").To4()),
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

	_, err = sender.Write(writer.Bytes())
	assert.NoError(t, err)

	sender.Close()

	// Listen for responses on all interfaces (receive broadcasts and unicasts)
	localPort := sender.LocalAddr().(*net.UDPAddr).Port
	listenAddr := &net.UDPAddr{IP: net.IPv4zero, Port: localPort}
	listener, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	listener.SetReadDeadline(time.Now().Add(5 * time.Second)) // Timeout
	buf2 := make([]byte, 2048)
	for {
		n, src, err := listener.ReadFromUDP(buf2)
		if err != nil {
			fmt.Println("Done listening (timeout or error):", err)
			break
		}
		fmt.Printf("Received %d bytes from %s: %s\n", n, src, string(buf2[:n]))
	}
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
