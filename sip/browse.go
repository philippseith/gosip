package sip

import (
	"braces.dev/errtrace"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

func SendBrowseRequest(senderIP string) error {
	ip := net.ParseIP(senderIP)
	if ip == nil {
		return errtrace.Wrap(fmt.Errorf("%w: invalid sender address %s", Error, senderIP))
	}
	writer := bytes.NewBuffer(make([]byte, 9))
	req := BrowseRequest{
		IPAddress:          [4]byte(ip.To4()),
		MasterOnly:         false,
		LowerSercosAddress: 0,
		UpperSercosAddress: 1024,
	}
	err := req.Write(writer)
	if err != nil {
		return errtrace.Wrap(err)
	}
	conn, err := net.ListenPacket("udp4", ":35021")
	if err != nil {
		return errtrace.Wrap(err)
	}
	defer func() { _ = conn.Close() }()

	broadcastAddr, err := net.ResolveUDPAddr("udp4", "255.255.255.255:35021")
	if err != nil {
		return errtrace.Wrap(err)
	}
	_, err = conn.WriteTo(writer.Bytes(), broadcastAddr)
	return errtrace.Wrap(err)
}

func ListenOnBrowseResponses(ctx context.Context) (<-chan Result[BrowseResponse], error) {
	conn, err := net.ListenPacket("udp4", ":35021")
	if err != nil {
		return nil, errtrace.Wrap(err)
	}
	ch := make(chan Result[BrowseResponse], 1024)
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
				err := conn.SetReadDeadline(time.Now().Add(time.Second))
				if err != nil {
					ch <- Err[BrowseResponse](errtrace.Wrap(err))
					// If setting the deadline does not work,
					// the go func might not end. We break here.
					return
				}
				buf := make([]byte, 1024)
				n, _, err := conn.ReadFrom(buf)
				if err != nil {
					// Timeouts are expected and used to check ctx.Done() in regular intervals
					if errors.Is(err, context.DeadlineExceeded) {
						continue
					}
					ch <- Err[BrowseResponse](errtrace.Wrap(err))
					// If ReadFrom errored with something different we need to stop
					return
				}
				resp := BrowseResponse{}
				err = resp.Read(bytes.NewReader(buf[:n]))
				if err == nil {
					ch <- Ok[BrowseResponse](resp)
				}
			}
		}
	}()
	return ch, nil
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
	return errtrace.Wrap(binary.Write(writer, binary.LittleEndian, *b))
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
