package sip

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/joomcode/errorx"
)

func Identify(ctx context.Context, interfaceName string, nodeIdentifier [6]byte) (chan Result[*IdentifyResponse], error) {

	writer := bytes.NewBuffer(make([]byte, 0, 14))

	ir := IdentifyRequest{
		NodeIdentifier: nodeIdentifier,
	}
	hdr := Header{
		TransactionID: 1,
		MessageType:   ir.MessageType(),
	}
	if err := hdr.Write(writer); err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	if err := ir.Write(writer); err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}

	ifc, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Can not read addresses of interface %s: %w", Error, interfaceName, err))
	}
	if len(addrs) == 0 {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Interface %s has no addresses", Error, interfaceName))
	}
	ipAddr, ok := addrs[0].(*net.IPNet)
	if !ok {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Can not parse address of interface %s: %v", Error, interfaceName, addrs[0]))
	}

	conn, err := net.DialUDP("udp", &net.UDPAddr{IP: ipAddr.IP, Port: 0}, &net.UDPAddr{IP: net.IPv4bcast, Port: 35021})
	if err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}

	return broadcast(ctx, []*net.UDPConn{conn}, writer.Bytes(),
		func(conn *net.UDPConn, ch chan<- Result[*IdentifyResponse]) bool {
			return listenUDP(conn, 1*time.Second, func() *IdentifyResponse {
				return &IdentifyResponse{}
			}, ch)
		}), nil
}

type IdentifyRequest struct {
	NodeIdentifier [6]byte
}

func (i *IdentifyRequest) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, i.NodeIdentifier); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (i *IdentifyRequest) MessageType() MessageType {
	return IdentifyRequestMsgType
}

type IdentifyResponse struct {
}

func (i *IdentifyResponse) Read(io.Reader) error {
	return nil
}

func (i *IdentifyResponse) Write(io.Writer) error {
	return nil
}

func (i *IdentifyResponse) MessageType() MessageType {
	return IdentifyResponseMsgType
}
