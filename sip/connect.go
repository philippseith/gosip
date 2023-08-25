package sip

import (
	"encoding/binary"
	"io"
	"net"
)

// Connect connect to a server over a connection with suggested timeouts for busy and lease.
// The used timeouts are returned in the response.
func Connect(conn net.Conn, busyTimeout, leaseTimeout int) (response ConnectResponse, ex Exception, err error) {
	request := &ConnectRequest{
		Version:      1,
		BusyTimeout:  uint32(busyTimeout),
		LeaseTimeout: uint32(leaseTimeout),
	}
	var header *Header
	header, err = sendRequestReceiveHeader(conn, request)
	ex, err = parseHeaderAndResponse(conn, header, err, &response)
	return response, ex, err
}

// ConnectRequest, MessageType 63
// In order to initiate a S/IP connection, the client sends a ConnectRequest PDU to the server.
// This request contains the desired S/IP version number and desired timeout values for the connection.
type ConnectRequest struct {
	// S/IP protocol version
	// version=1 shall be used for this protocol version
	Version uint32
	// requested busy-timeout the server should use, in milliseconds
	BusyTimeout uint32
	// requested lease-timeout the server should use, in milliseconds
	LeaseTimeout uint32
}

func (c *ConnectRequest) Read(reader io.Reader) error {
	return binary.Read(reader, binary.LittleEndian, c)
}

func (c *ConnectRequest) Write(writer io.Writer) error {
	return binary.Write(writer, binary.LittleEndian, *c)
}

func (c *ConnectRequest) MessageType() uint32 {
	return ConnectRequestMsgType
}

type ConnectResponse struct {
	connectResponse
	// supported Request MessageTypes of the server on this TCP connection.
	// The client must only use these message types in a request.
	MessageTypes []uint32
}

type connectResponse struct {
	// S/IP protocol version
	// version=1 shall be used for this protocol version
	Version uint32
	// requested busy-timeout the server is using, in milliseconds
	BusyTimeout uint32
	// requested lease-timeout the server is using, in milliseconds
	LeaseTimeout uint32
	// number of supported Request MessageTypes
	NoMessageTypes uint32
}

func (c *ConnectResponse) Read(reader io.Reader) error {
	err := binary.Read(reader, binary.LittleEndian, &c.connectResponse)
	if err != nil {
		return err
	}
	c.MessageTypes = make([]uint32, c.NoMessageTypes)
	return binary.Read(reader, binary.LittleEndian, c.MessageTypes)
}

func (c *ConnectResponse) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, c.connectResponse)
	if err != nil || c.NoMessageTypes == 0 {
		return err
	}
	return binary.Write(writer, binary.LittleEndian, c.MessageTypes)
}

func (c *ConnectResponse) MessageType() uint32 {
	return ConnectResponseMsgType
}
