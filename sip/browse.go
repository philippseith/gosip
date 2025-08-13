package sip

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/joomcode/errorx"
)

// Browse listens to BrowseResponses and broadcasts one BrowseRequest on the given interface.
// The Listening ends when ctx is canceled.
func Browse(ctx context.Context, interfaceName string) (chan Result[*BrowseResponse], error) {

	reqConns, err := getReqConnsForIfc(interfaceName)
	if err != nil {
		return nil, err
	}

	browseRequest, err := buildBrowseRequest()
	if err != nil {
		return nil, err
	}

	ch := make(chan Result[*BrowseResponse], 512) // Such many devices should be a pretty uncommon case
	var wg sync.WaitGroup

	for _, reqConn := range reqConns {
		wg.Add(1)

		go func() {
			defer wg.Done()

			localPort := reqConn.LocalAddr().(*net.UDPAddr).Port
			_, err := reqConn.Write(browseRequest)
			if err != nil {
				ch <- Err[*BrowseResponse](errorx.EnsureStackTrace(err))
				return
			}
			// The drives are responding on our local port but with the broadcast address.
			// To allow listening on the port, we need to close the sending connection
			// and open a new one for listening.
			reqConn.Close()

			listenAddr := &net.UDPAddr{IP: net.IPv4zero, Port: localPort}
			respConn, err := net.ListenUDP("udp", listenAddr)
			if err != nil {
				ch <- Err[*BrowseResponse](errorx.EnsureStackTrace(err))
				return
			}
			defer respConn.Close()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Blocks until a reponse comes in or a 1 sec timeout elapses
					if !listenUDP(respConn, time.Second, func() *BrowseResponse {
						return &BrowseResponse{}
					}, ch) {
						return
					}
				}
			}

		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch, nil
}

func getReqConnsForIfc(interfaceName string) (reqConns []*net.UDPConn, err error) {
	ifcs, err := net.Interfaces()
	if err != nil {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Can not read system interfaces %w", Error, err))
	}

	for _, ifc := range ifcs {
		if ifc.Name != interfaceName {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Can not read addresses of interface %s: %w", Error, interfaceName, err))
		}
		for _, addr := range addrs {
			reqConn, err := addrToReqConn(addr)
			if err != nil {
				return nil, err
			}
			if reqConn == nil {
				continue
			}
			reqConns = append(reqConns, reqConn)
		}
	}

	if len(reqConns) == 0 {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("interface %s has no ipv4 addresses", interfaceName))
	}
	return reqConns, nil
}

func addrToReqConn(addr net.Addr) (*net.UDPConn, error) {
	ipAddr, ok := addr.(*net.IPNet)
	if !ok {
		return nil, nil
	}
	ip := ipAddr.IP.To4()
	if ip == nil {
		return nil, nil
	}
	mask := ipAddr.Mask
	broadcast := make(net.IP, 4)
	for i := range 4 {
		broadcast[i] = ip[i] | ^mask[i]
	}
	localAddr := &net.UDPAddr{IP: ip, Port: 0}
	broadcastAddr := &net.UDPAddr{IP: broadcast, Port: 35021}

	reqConn, err := net.DialUDP("udp", localAddr, broadcastAddr)
	if err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	return reqConn, nil
}

func buildBrowseRequest() ([]byte, error) {
	writer := bytes.NewBuffer(make([]byte, 0, 17))
	br := BrowseRequest{
		IPAddress:          [4]byte(net.IPv4bcast),
		MasterOnly:         false,
		LowerSercosAddress: 0,
		UpperSercosAddress: 511,
	}
	hdr := Header{
		TransactionID: 1,
		MessageType:   br.MessageType(),
	}
	if err := hdr.Write(writer); err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	if err := br.Write(writer); err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	return writer.Bytes(), nil
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
