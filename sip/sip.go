package sip

import (
	"fmt"
	"io"
	"net"
)

// Port is the default SIP port
const Port = 35021

// PDU can be read from bytes and written to bytes and have a message type
type PDU interface {
	Read(io.Reader) error
	Write(io.Writer) error
	MessageType() uint32
}

var transactionId uint32

func sendRequestReceiveHeader[Req PDU](conn net.Conn, req Req) (header *Header, err error) {
	transactionId++
	tId := transactionId
	header = &Header{
		TransactionID: tId,
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
	if header.TransactionID != tId {
		return header, fmt.Errorf("wrong TransactionID. Expected %d, got %d", tId, header.TransactionID)
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
