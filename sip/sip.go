package sip

import (
	"errors"
	"fmt"
	"io"
)

// Port is the default SIP port
const Port = 35021

// General S/IP error. Base of all other S/IP errors
var Error = errors.New("S/IP")

var ErrorTimeout = fmt.Errorf("%w: Timeout", Error)
var ErrorClosed = fmt.Errorf("%w: Connection closed", Error)
var ErrorInvalidResponseMessageType = fmt.Errorf("%w: Invalid response message type", Error)
var ErrorWrongTransactionID = fmt.Errorf("%w: Wrong TransactionID", Error)
var ErrorRetriesExceeded = fmt.Errorf("%w: Retries for reconnect exceeded", Error)

// PDU can be read from bytes and written to bytes and have a message type
type PDU interface {
	Read(io.Reader) error
	Write(io.Writer) error
	MessageType() MessageType
}
