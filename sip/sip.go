package sip

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
)

// Port is the default SIP port
const Port = 35021

// General S/IP error. Base of all other S/IP errors
var Error = errors.New("S/IP")

var ErrorTimeout = fmt.Errorf("%w: timeout", Error)
var ErrorClosed = fmt.Errorf("%w: connection closed", Error)
var ErrorInvalidResponseMessageType = fmt.Errorf("%w: invalid response message type", Error)
var ErrorWrongTransactionID = fmt.Errorf("%w: wrong TransactionID", Error)

// PDU can be read from bytes and written to bytes and have a message type
type PDU interface {
	Read(io.Reader) error
	Write(io.Writer) error
	MessageType() uint32
}

var transactionID uint32

func sendRequestReceiveHeader[Req PDU](conn net.Conn, req Req) (header *Header, err error) {
	tID := atomic.AddUint32(&transactionID, 1)
	header = &Header{
		TransactionID: tID,
		MessageType:   req.MessageType(),
	}
	if err = header.Write(conn); err != nil {
		return header, err
	}
	if err = req.Write(conn); err != nil {
		return header, err
	}
	err = header.Read(conn)
	if err != nil {
		return header, err
	}
	if header.TransactionID != tID {
		return header, fmt.Errorf("%w. Expected %d, got %d", ErrorWrongTransactionID, tID, header.TransactionID)
	}
	return header, err
}

func parseHeaderAndResponse[Resp PDU](reader io.Reader, header *Header, err error, response Resp) (Exception, error) {
	var ex Exception
	if err != nil {
		return ex, err
	}
	switch header.MessageType {
	case response.MessageType():
		err = response.Read(reader)
		return ex, err
	case ExceptionMsgType:
		err = ex.Read(reader)
		return ex, err
	default:
		return ex, fmt.Errorf("invalid connect response messagetype %d", header.MessageType)
	}
}
