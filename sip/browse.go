package sip

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/joomcode/errorx"
)

// Browse listens to BrowseResponses and broadcasts one BrowseRequest on the given interface.
// The Listening ends when ctx is canceled.
//
// Example:
//  ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
//  defer cancel()
//  resCh, err := sip.Browse(ctx, "en0")
//  if err != nil { log.Fatal(err) }
//  for res := range resCh { fmt.Println(res) }

func Browse(ctx context.Context, interfaceName string) (chan Result[*BrowseResponse], error) {
	return Broadcast[*BrowseResponse](ctx, interfaceName, &BrowseRequest{
		IPAddress:          [4]byte(net.IPv4bcast),
		MasterOnly:         false,
		LowerSercosAddress: 0,
		UpperSercosAddress: 511,
	}, time.Second)
}

type BrowseRequest struct {
	IPAddress          [4]byte
	MasterOnly         bool
	LowerSercosAddress uint16
	UpperSercosAddress uint16
}

func (b *BrowseRequest) Read(reader io.Reader) error {
	if err := binary.Read(reader, binary.LittleEndian, b); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (b *BrowseRequest) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, b.IPAddress); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if err := binary.Write(writer, binary.LittleEndian, b.MasterOnly); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if err := binary.Write(writer, binary.LittleEndian, b.LowerSercosAddress); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if err := binary.Write(writer, binary.LittleEndian, b.UpperSercosAddress); err != nil {
		return errorx.EnsureStackTrace(err)
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
		return errorx.EnsureStackTrace(err)
	}
	if b.DisplayNameLength > maxPDUFieldLength {
		return errorx.EnsureStackTrace(fmt.Errorf("%w: DisplayNameLength %d exceeds maximum %d", Error, b.DisplayNameLength, maxPDUFieldLength))
	}
	b.DisplayName = make([]byte, b.DisplayNameLength)
	err = binary.Read(reader, binary.LittleEndian, b.DisplayName)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	err = binary.Read(reader, binary.LittleEndian, &b.HostNameLength)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if b.HostNameLength > maxPDUFieldLength {
		return errorx.EnsureStackTrace(fmt.Errorf("%w: HostNameLength %d exceeds maximum %d", Error, b.HostNameLength, maxPDUFieldLength))
	}
	b.HostName = make([]byte, b.HostNameLength)
	err = binary.Read(reader, binary.LittleEndian, b.HostName)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (b *BrowseResponse) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, b.browseResponse)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if b.DisplayNameLength > 0 {
		err = binary.Write(writer, binary.LittleEndian, b.DisplayName)
		if err != nil {
			return errorx.EnsureStackTrace(err)
		}
	}
	err = binary.Write(writer, binary.LittleEndian, b.HostNameLength)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if b.HostNameLength > 0 {
		if err = binary.Write(writer, binary.LittleEndian, b.HostName); err != nil {
			return errorx.EnsureStackTrace(err)
		}
	}
	return nil
}

func (b *BrowseResponse) MessageType() MessageType {
	return BrowseResponseMsgType
}
