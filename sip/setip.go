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

// SetIP broadcasts a SetIPRequest on the specified network interface
// and listens for SetIPResponses  from devices. Listening ends when ctx is canceled.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
//	defer cancel()
//	resCh, err := sip.SetIP(ctx, "en0", [6]byte{1,2,3,4,5,6}, net.IPv4(192,168,1,100), net.IPv4(255,255,255,0), net.IPv4(192,168,1,1), true)
//	if err != nil { log.Fatal(err) }
//	for res := range resCh { fmt.Println(res) }
func SetIP(ctx context.Context, interfaceName string, nodeIdentifier [6]byte, ip net.IP, gateway net.IP, persist bool) (chan Result[*SetIPResponse], error) {
	var persitentByte byte
	if persist {
		persitentByte = 1
	}
	return Broadcast[*SetIPResponse](ctx, interfaceName, &SetIPRequest{
		setIPRequest: setIPRequest{
			NodeIdentifier: nodeIdentifier,
			MACAddress:     nodeIdentifier,
			IPAddress:      [4]byte(ip.To4()),
			Subnet:         [4]byte(ip.To4().DefaultMask()),
			Gateway:        [4]byte(gateway.To4()),
			Persistent:     persitentByte,
		},
	}, time.Second)
}

type SetIPRequest struct {
	setIPRequest

	HostName []byte
}

type setIPRequest struct {
	NodeIdentifier [6]byte
	MACAddress     [6]byte
	DHCPMode       byte
	IPAddress      [4]byte
	Subnet         [4]byte
	Gateway        [4]byte
	Persistent     byte
	HostNameLength uint32
}

func (s *SetIPRequest) Read(reader io.Reader) error {
	err := binary.Read(reader, binary.LittleEndian, &s.setIPRequest)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if s.HostNameLength > maxPDUFieldLength {
		return errorx.EnsureStackTrace(fmt.Errorf("%w: HostNameLength %d exceeds maximum %d", Error, s.HostNameLength, maxPDUFieldLength))
	}
	s.HostName = make([]byte, s.HostNameLength)
	err = binary.Read(reader, binary.LittleEndian, s.HostName)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (s *SetIPRequest) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, s.setIPRequest); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if s.HostNameLength > 0 {
		if err := binary.Write(writer, binary.LittleEndian, s.HostName); err != nil {
			return errorx.EnsureStackTrace(err)
		}
	}
	return nil
}

func (s *SetIPRequest) MessageType() MessageType {
	return SetIPRequestMsgType
}

type SetIPResponse struct {
}

func (s *SetIPResponse) Read(io.Reader) error {
	return nil
}

func (s *SetIPResponse) Write(io.Writer) error {
	return nil
}

func (s *SetIPResponse) MessageType() MessageType {
	return SetIPResponseMsgType
}
