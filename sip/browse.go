package sip

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/joomcode/errorx"
)

// ListenToBrowseResponses listens on interfaceName for BrowseResponses until ctx is canceled.
// It automatically reserves a free port for listening.
// The returned net.PacketConn should be used to send BrowseRequests on the listened port.
// The channel returns any valid BrowseResponse that is received, all errors occured while parsing received packages
// and all errors with the net.PacketConn. The connection and the channel are closed in case of errors of the latter category.
func ListenToBrowseResponses(ctx context.Context, interfaceName string) ([]net.PacketConn, <-chan Result[*BrowseResponse], error) {
	conns, err := connsForInterface(interfaceName)
	if err != nil {
		return nil, nil, err
	}

	ch := make(chan Result[*BrowseResponse], 512) // Such many devices should be a pretty uncommon case
	var wg sync.WaitGroup

	for _, conn := range conns {
		wg.Add(1)

		go func(conn net.PacketConn) {
			defer func() {
				wg.Done()
				if conn != nil {
					_ = conn.Close()
				}
			}()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Blocks until a reponse comes in or a 1 sec timeout elapses
					if !listenUDP(conn, time.Second, func() *BrowseResponse {
						return &BrowseResponse{}
					}, ch) {
						return
					}
				}
			}
		}(conn)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return conns, ch, nil
}

// BroadcastBrowseRequest broadcasts a BrowseRequest on the passed net.PacketConn from ListenToBrowseResponses.
// Note that the net.PacketConn will be closed when ListenToBrowseResponses is canceled.
func BroadcastBrowseRequest(conn net.PacketConn) error {
	return sendBrowseRequest(conn, "255.255.255.255")
}

// SendBrowseRequest sends a BrowseRequest on the passed net.PacketConn to a dedicated IP address.
func SendBrowseRequest(conn net.PacketConn, address string) error {
	if net.ParseIP(address) == nil {
		return errorx.EnsureStackTrace(fmt.Errorf("%w: %s is not a valid IP address", Error, address))
	}
	return sendBrowseRequest(conn, address)
}

func sendBrowseRequest(conn net.PacketConn, address string) error {
	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return errorx.EnsureStackTrace(fmt.Errorf("%w: can not convert to net.UDPAddr: %s", Error, conn.LocalAddr().String()))
	}
	req := &BrowseRequest{
		IPAddress:          [4]byte(udpAddr.IP.To4()),
		MasterOnly:         false,
		LowerSercosAddress: 0,
		UpperSercosAddress: 511,
	}
	return sendUDP(conn, address, req)
}

// Browse listens to BrowseResponses and broadcasts one BrowseRequest on the given interface.
// The Listening ends when ctx is canceled.
func Browse(ctx context.Context, interfaceName string) (<-chan Result[*BrowseResponse], error) {
	conns, ch, err := ListenToBrowseResponses(ctx, interfaceName)
	if err != nil {
		return nil, err
	}

	var allErr error
	for _, conn := range conns {
		allErr = errors.Join(allErr, BroadcastBrowseRequest(conn))
	}
	return ch, allErr
}

func connsForInterface(interfaceName string) ([]net.PacketConn, error) {
	ips, err := findIPV4OfInterface(interfaceName)
	if err != nil {
		return nil, err
	}

	var allErr error
	conns := make([]net.PacketConn, 0, len(ips))
	for _, ip := range ips {

		conn, err := newUDPConn(ip)
		if err != nil {
			allErr = errors.Join(allErr, err)
			continue
		}

		conns = append(conns, conn)
	}
	return conns, allErr
}

func findIPV4OfInterface(interfaceName string) ([]net.IP, error) {
	ifcs, err := net.Interfaces()
	if err != nil {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Can not read system interfaces %w", Error, err))
	}
	var ipV4s []net.IP
	for _, ifc := range ifcs {
		if ifc.Name != interfaceName {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Can not read addresses of interface %s: %w", Error, interfaceName, err))
		}
		for _, addr := range addrs {
			ipAddr, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipAddr.IP.To4() != nil {
				ipV4s = append(ipV4s, ipAddr.IP)
			}
		}
	}
	if len(ipV4s) == 0 {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Can find IP for interface %s: %w", Error, interfaceName, err))
	}
	return ipV4s, nil
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
	b.DisplayName = make([]byte, b.DisplayNameLength)
	err = binary.Read(reader, binary.LittleEndian, b.DisplayName)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	err = binary.Read(reader, binary.LittleEndian, &b.HostNameLength)
	if err != nil {
		return errorx.EnsureStackTrace(err)
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
