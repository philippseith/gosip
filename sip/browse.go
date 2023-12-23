package sip

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"braces.dev/errtrace"
)

// ListenToBrowseResponses listens on interfaceIP for BrowseResponses until ctx is canceled.
// It automatically reserves a free port for listening.
// The returned net.PacketConn should be used to send BrowseRequests on the listened port.
// The channel returns any valid BrowseResponse that is received, all errors occured while parsing received packages
// and all errors with the net.PacketConn. The connection and the channel are closed in case of errors of the latter category.
func ListenToBrowseResponses(ctx context.Context, interfaceIP string) (net.PacketConn, <-chan Result[BrowseResponse], error) {
	conn, _, err := newConn(interfaceIP)
	if err != nil {
		return nil, nil, errtrace.Wrap(err)
	}
	ch := make(chan Result[BrowseResponse], 512) // Such many devices should be a pretty uncommon case
	go func() {
		defer func() {
			_ = conn.Close()
			close(ch)
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Blocks until a reponse comes in or a 1 sec timeout elapses
				if !listenToBrowseResponse(conn, ch) {
					return
				}
			}
		}
	}()

	return conn, ch, nil
}

// SendBrowseRequest sends a BrowseRequest on the passed net.PacketConn from ListenToBrowseResponses.
// Note that the net.PacketConn will be closed when ListenToBrowseResponses is canceled.
func SendBrowseRequest(conn net.PacketConn) error {
	writer := bytes.NewBuffer(make([]byte, 0, 17))
	// Write header to buffer
	hdr := Header{
		TransactionID: 1,
		MessageType:   BrowseRequestMsgType,
	}
	err := hdr.Write(writer)
	if err != nil {
		return errtrace.Wrap(err)
	}
	// Write BrowseRequest to buffer
	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return errtrace.Wrap(fmt.Errorf("can not convert to net.UDPAddr: %s", conn.LocalAddr().String()))
	}
	req := BrowseRequest{
		IPAddress:          [4]byte(udpAddr.IP.To4()),
		MasterOnly:         false,
		LowerSercosAddress: 0,
		UpperSercosAddress: 511,
	}
	err = req.Write(writer)
	if err != nil {
		return errtrace.Wrap(err)
	}
	// Create broadcast address
	broadcastAddr, err := net.ResolveUDPAddr("udp4", "255.255.255.255:35021")
	if err != nil {
		return errtrace.Wrap(err)
	}
	// Send the request
	_, err = conn.WriteTo(writer.Bytes(), broadcastAddr)
	return errtrace.Wrap(err)
}

// Browse calls ListenToBrowseResponses and sends one BrowseRequest.
func Browse(ctx context.Context, interfaceIP string) (<-chan Result[BrowseResponse], error) {
	conn, ch, err := ListenToBrowseResponses(ctx, interfaceIP)
	if err != nil {
		return nil, errtrace.Wrap(err)
	}
	if err := SendBrowseRequest(conn); err != nil {
		return nil, errtrace.Wrap(err)
	}
	return ch, nil
}

func newConn(interfaceIP string) (net.PacketConn, net.IP, error) {
	ip := net.ParseIP(interfaceIP)
	if ip == nil {
		return nil, nil, errtrace.Wrap(fmt.Errorf("%w: invalid sender address %s", Error, interfaceIP))
	}
	listenAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:0", ip.String()))
	if err != nil {
		return nil, nil, errtrace.Wrap(err)
	}
	conn, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return nil, nil, errtrace.Wrap(err)
	}
	return conn, ip, nil
}

func listenToBrowseResponse(conn net.PacketConn, ch chan<- Result[BrowseResponse]) bool {
	err := conn.SetReadDeadline(time.Now().Add(time.Second))
	if err != nil {
		ch <- Err[BrowseResponse](errtrace.Wrap(err))
		// If setting the deadline does not work,
		// the go func might not end. We break here.
		return false
	}
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		// Timeouts are expected and used to check ctx.Done() in regular intervals
		// See https://pkg.go.dev/net@go1.18.3#Conn.SetDeadline
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return true
		}
		ch <- Err[BrowseResponse](errtrace.Wrap(err))
		// If ReadFrom errored with something different we need to stop
		return false
	}
	reader := bytes.NewReader(buf[:n])
	hdr := Header{}
	err = hdr.Read(reader)
	if err != nil || hdr.MessageType != BrowseResponseMsgType {
		return true
	}
	resp := BrowseResponse{}
	err = resp.Read(reader)
	if err == nil {
		ch <- Ok[BrowseResponse](resp)
	} else {
		ch <- Err[BrowseResponse](errtrace.Wrap(fmt.Errorf(
			"%w: Can not parse packet as BrowseResponse %v: %w", Error, buf[:n], err)))
		// Do not end the listening, there might come more (valid) responses
	}
	return true
}

type BrowseRequest struct {
	IPAddress          [4]byte
	MasterOnly         bool
	LowerSercosAddress uint16
	UpperSercosAddress uint16
}

func (b *BrowseRequest) Read(reader io.Reader) error {
	return errtrace.Wrap(binary.Read(reader, binary.LittleEndian, b))
}

func (b *BrowseRequest) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, b.IPAddress); err != nil {
		return errtrace.Wrap(err)
	}
	if err := binary.Write(writer, binary.LittleEndian, b.MasterOnly); err != nil {
		return errtrace.Wrap(err)
	}
	if err := binary.Write(writer, binary.LittleEndian, b.LowerSercosAddress); err != nil {
		return errtrace.Wrap(err)
	}
	if err := binary.Write(writer, binary.LittleEndian, b.UpperSercosAddress); err != nil {
		return errtrace.Wrap(err)
	}
	return nil
}

func (b *BrowseRequest) MessageType() MessageType {
	return BrowseRequestMsgType
}

type BrowseResponse struct {
	browseResponse

	DisplayName []byte

	HostNameLength uint32
	HostName       []byte
}
type browseResponse struct {
	Version uint32

	NodeIdentifier [6]byte
	MacAddress     [6]byte

	DHCPFeatures byte
	DHCPMode     byte

	IPAddress [4]byte
	Subnet    [4]byte
	Gateway   [4]byte

	DisplayNameLength uint32
}

func (b *BrowseResponse) Read(reader io.Reader) error {
	err := binary.Read(reader, binary.LittleEndian, &b.browseResponse)
	if err != nil {
		return errtrace.Wrap(err)
	}
	b.DisplayName = make([]byte, b.DisplayNameLength)
	err = binary.Read(reader, binary.LittleEndian, b.DisplayName)
	if err != nil {
		return errtrace.Wrap(err)
	}
	err = binary.Read(reader, binary.LittleEndian, &b.HostNameLength)
	if err != nil {
		return errtrace.Wrap(err)
	}
	b.HostName = make([]byte, b.HostNameLength)
	return errtrace.Wrap(binary.Read(reader, binary.LittleEndian, b.HostName))
}

func (b *BrowseResponse) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, b.browseResponse)
	if err != nil {
		return errtrace.Wrap(err)
	}
	if b.DisplayNameLength > 0 {
		err = binary.Write(writer, binary.LittleEndian, b.DisplayName)
		if err != nil {
			return errtrace.Wrap(err)
		}
	}
	err = binary.Write(writer, binary.LittleEndian, b.HostNameLength)
	if err != nil {
		return errtrace.Wrap(err)
	}
	if b.HostNameLength > 0 {
		return errtrace.Wrap(binary.Write(writer, binary.LittleEndian, b.HostName))
	}
	return nil
}

func (b *BrowseResponse) MessageType() MessageType {
	return BrowseResponseMsgType
}
