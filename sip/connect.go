package sip

import (
	"encoding/binary"
	"io"

	"braces.dev/errtrace"
)

// ConnectRequest MessageType 63
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
	return errtrace.Wrap(binary.Read(reader, binary.LittleEndian, c))
}

func (c *ConnectRequest) Write(writer io.Writer) error {
	return errtrace.Wrap(binary.Write(writer, binary.LittleEndian, *c))
}

func (c *ConnectRequest) MessageType() MessageType {
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
		return errtrace.Wrap(err)
	}
	c.MessageTypes = make([]uint32, c.NoMessageTypes)
	return errtrace.Wrap(binary.Read(reader, binary.LittleEndian, c.MessageTypes))
}

func (c *ConnectResponse) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, c.connectResponse)
	if err != nil || c.NoMessageTypes == 0 {
		return errtrace.Wrap(err)
	}
	return errtrace.Wrap(binary.Write(writer, binary.LittleEndian, c.MessageTypes))
}

func (c *ConnectResponse) MessageType() MessageType {
	return ConnectResponseMsgType
}
