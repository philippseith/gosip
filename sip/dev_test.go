package sip

import (
	"bytes"
	"context"
	"encoding/binary"
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
	conn, err := Dial(context.Background(), "tcp", "192.168.112.14:35021", WithBusyTimeout(5000))
	assert.NoError(t, err)

	resp, err := conn.ReadOnlyData(context.Background(), 0, 0, 0x8000+1354)
	assert.NoError(t, err)

	err = conn.WriteData(context.Background(), 0, 0, 0x8000+1354, resp.Data)
}

func TestDecodeReadEverythingP1518(t *testing.T) {
	b := []byte{0x44, 0x0, 0x0, 0x0, 0x46, 0x0, 0x0, 0x0, 0x47, 0x0, 0x0, 0x0, 0x2e, 0x0, 0x1, 0x0, 0x36, 0x70, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xfc, 0x0, 0x0, 0x0, 0x44, 0x0, 0x0, 0x0, 0x43, 0x6f, 0x6e, 0x74, 0x72, 0x6f, 0x6c, 0x20, 0x73, 0x65, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x20, 0x64, 0x61, 0x74, 0x61, 0x3a, 0x20, 0x41, 0x78, 0x69, 0x73, 0x20, 0x74, 0x79, 0x70, 0x65, 0x20, 0x64, 0x61, 0x74, 0x61, 0x20, 0x69, 0x6e, 0x20, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x1, 0x0, 0x1, 0x0, 0x3c, 0x0, 0x0, 0x0, 0xc, 0x0, 0x0, 0x0, 0x8, 0x0, 0x8, 0x0, 0x43, 0x54, 0x52, 0x2d, 0x41, 0x58, 0x49, 0x53, 0x8d, 0x8a, 0x1, 0x0, 0x1, 0x0, 0x15, 0x70, 0x4, 0x0, 0x4, 0x0, 0x1, 0x0, 0x1, 0x0, 0x8d, 0x8a, 0x2, 0x0, 0x1, 0x0, 0x15, 0x70, 0x4, 0x0, 0x4, 0x0, 0x1, 0x0, 0x1, 0x0, 0x32, 0x8a, 0x2b, 0x0, 0x1, 0x0, 0x62, 0x70, 0x0, 0x0, 0x40, 0x40}

	reader := bytes.NewBuffer(b)

	header := Header{}
	err := header.Read(reader)
	assert.NoError(t, err)

	resp := ReadEverythingResponse{}
	err = resp.Read(reader)
	assert.NoError(t, err)
	log.Print(resp)
}

func TestListenUDPMalformedPacketErrorIsBounded(t *testing.T) {
	packet := make([]byte, 65507)
	binary.LittleEndian.PutUint32(packet[4:8], uint32(ReadEverythingResponseMsgType))
	binary.LittleEndian.PutUint16(packet[12:14], 0xFFFF)
	conn := &singlePacketConn{packet: packet}
	results := make(chan Result[*ReadEverythingResponse], 1)

	if !listenUDP(context.Background(), conn, time.Second, results) {
		t.Fatal("malformed response unexpectedly stopped the listener")
	}
	result := <-results
	if result.Err == nil {
		t.Fatal("malformed response unexpectedly parsed successfully")
	}
	if len(result.Err.Error()) > 512 {
		t.Fatalf("malformed packet error retained full input: %d bytes", len(result.Err.Error()))
	}
}

func TestListenUDPCanceledResultDeliveryUnblocks(t *testing.T) {
	packet := make([]byte, 8+36)
	binary.LittleEndian.PutUint32(packet[4:8], uint32(ReadEverythingResponseMsgType))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn := &singlePacketConn{packet: packet}
	results := make(chan Result[*ReadEverythingResponse])
	done := make(chan bool, 1)
	go func() {
		done <- listenUDP(ctx, conn, time.Second, results)
	}()

	select {
	case keepListening := <-done:
		if keepListening {
			t.Fatal("listener continued after canceled result delivery")
		}
	case <-time.After(time.Second):
		t.Fatal("result delivery remained blocked after cancellation")
	}
}

type singlePacketConn struct {
	packet []byte
}

func (c *singlePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	return copy(p, c.packet), &net.UDPAddr{}, nil
}

func (*singlePacketConn) WriteTo(p []byte, _ net.Addr) (int, error) { return len(p), nil }
func (*singlePacketConn) Close() error                              { return nil }
func (*singlePacketConn) LocalAddr() net.Addr                       { return &net.UDPAddr{} }
func (*singlePacketConn) SetDeadline(time.Time) error               { return nil }
func (*singlePacketConn) SetReadDeadline(time.Time) error           { return nil }
func (*singlePacketConn) SetWriteDeadline(time.Time) error          { return nil }
