// Package sip implements the Sercos Internet Protocol (S/IP), used for communicating
// and managing parameter data over Sercos devices via TCP and UDP.
// Features include reading/writing parameters, device browsing, multiplexing for servers,
// automatic reconnect clients, and error handling with stack traces.
// Use NewClient for a reconnecting client, or Serve to host a S/IP server.
// See examples on exported functions for typical usage.
package sip

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
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
