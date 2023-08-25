package sip

import (
	"encoding/binary"
	"io"
)

// Exception, MessageType: 67.
// It contains a common error code and an optional service specific error code.
type Exception struct {
	CommomErrorCode   uint16
	SpecificErrorCode uint32
}

func (c *Exception) Read(reader io.Reader) error {
	return binary.Read(reader, binary.LittleEndian, c)
}

func (c *Exception) Write(writer io.Writer) error {
	return binary.Write(writer, binary.LittleEndian, *c)
}

func (c *Exception) MessageType() uint32 {
	return ExceptionMsgType
}

// the server is not able to serve a TCP based S/IP connection. See TCP based communication initialization for further details.
const ConnectionError = uint16(1)

// a timeout exceeds (see Timeouts for further details) or a TCP connection gets lost.
// Network activities are controlled by local timeout handling.
// If the server doesn't respond in time, this error code is used to indicate the error to the user on client-side.
const TimeoutError = uint16(2)

// s the server receives an unknown message type, it shall send an exception with this error code to the client.
// In case of a TCP based S/IP request the server returns the exception to the client and shall close the TCP stream socket connection.
const UnknownMessageTypeError = uint16(3)

// Services are able to have their own error code. Further information are available in the SpecificErrorCode of the Exception structure.
// See also specific error code descriptions of the service invoked.
const ServiceSpecificError = uint16(4)

// request or response does not fit to the UDP datagram (limitation of PDU size)
const PduToLargeError = uint16(5)

// malformed PDU e.g. received UDP datagram does not correspond to the expected PDU size
const PduProtocolMismatchError = uint16(5)
