// Package sip implements the Sercos Internet Protocol (S/IP), used for communicating
// and managing parameter data over Sercos devices via TCP and UDP.
// Features include reading/writing parameters, device browsing, multiplexing for servers,
// automatic reconnect clients, and error handling with stack traces.
// Use NewClient for a reconnecting client, or Serve to host a S/IP server.
// See examples on exported functions for typical usage.
package sip

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/joomcode/errorx"
)

// Port is the default SIP port
const Port = 35021

// maxPDUFieldLength is the maximum byte length of any variable-length field
// in an S/IP PDU (data, name, unit, display name, host name, number of message
// types). Capped at 0xFFFF (65535) per the Sercos IDN parameter model.
const maxPDUFieldLength uint32 = 0xFFFF

// Error defines the S/IP error class. Base of all other S/IP errors
var Error = errors.New("S/IP")

var ErrorTimeout = fmt.Errorf("%w: Timeout", Error)
var ErrorClosed = fmt.Errorf("%w: Connection closed", Error)
var ErrorInvalidRequestMessageType = fmt.Errorf("%w: Invalid request message type", Error)
var ErrorInvalidResponseMessageType = fmt.Errorf("%w: Invalid response message type", Error)
var ErrorRetriesExceeded = fmt.Errorf("%w: Reconnect timeout exceeded", Error)

// PDU can be read from bytes and written to bytes and have a message type
type PDU interface {
	Read(io.Reader) error
	Write(io.Writer) error
	MessageType() MessageType
}

// MarshalPDU takes a PDU and returns its byte representation, including the header.
func MarshalPDU[T PDU](pdu T) ([]byte, error) {
	writer := bytes.NewBuffer(nil)
	hdr := Header{
		TransactionID: 1,
		MessageType:   pdu.MessageType(),
	}
	if err := hdr.Write(writer); err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	if err := pdu.Write(writer); err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	return writer.Bytes(), nil
}

// NewResponse returns an empty response PDU for the given message type.
// It returns the zero value of T if the message type has no response PDU
// implementation or if that implementation is not assignable to T.
func NewResponse[T PDU](msgType MessageType) T { // nolint:cyclop
	var response PDU
	switch msgType {
	case ConnectResponseMsgType:
		response = &ConnectResponse{}
	case PingResponseMsgType:
		response = &PingResponse{}
	case ExceptionMsgType:
		response = &Exception{}
	case ReadEverythingResponseMsgType:
		response = &ReadEverythingResponse{}
	case ReadOnlyDataResponseMsgType:
		response = &ReadOnlyDataResponse{}
	case ReadDescriptionResponseMsgType:
		response = &ReadDescriptionResponse{}
	case WriteDataResponseMsgType:
		response = &WriteDataResponse{}
	case ReadDataStateResponseMsgType:
		response = &ReadDataStateResponse{}
	case IdentifyResponseMsgType:
		response = &IdentifyResponse{}
	case SetIPResponseMsgType:
		response = &SetIPResponse{}
	case BrowseResponseMsgType:
		response = &BrowseResponse{}
	}

	typed, ok := response.(T)
	if !ok {
		var zero T
		return zero
	}
	return typed
}

// MessageTypeOf returns the message type of T without needing an instance of it.
// T needs to be an actual PDU type that implements the MessageType method, not simple the PDU interface.
func MessageTypeOf[T PDU]() MessageType {
	var zero T
	return zero.MessageType()
}

type RequestPDU interface {
	PDU
	Target() Request
}

// Request is the address part of a PDU
type Request struct {
	SlaveIndex     uint16
	SlaveExtension uint16
	IDN            uint32
}

var logger = log.New(io.Discard, "sip: ", log.Ldate|log.Lmicroseconds|log.Lmsgprefix)

// EnableLogging sets the logger output to os.Stderr if enable is true, otherwise it discards the log output
func EnableLogging(enable bool) {
	if enable {
		logger.SetOutput(os.Stderr)
	} else {
		logger.SetOutput(io.Discard)
	}
}

// Constants for the validElements field in ReadDescriptionResponse and ReadEverythingResponse
const (
	ElmDataState uint16 = 0x0001
	ElmName      uint16 = 0x0002
	ElmAttribute uint16 = 0x0004
	ElmUnit      uint16 = 0x0008
	ElmMin       uint16 = 0x0010
	ElmMax       uint16 = 0x0020
	ElmData      uint16 = 0x0040
)
