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

// Request is the address part of a PDU
type Request struct {
	SlaveIndex     uint16
	SlaveExtension uint16
	IDN            uint32
}

var logger = log.New(os.Stderr, "sip: ", log.Ldate|log.Lmicroseconds|log.Lmsgprefix)
