package sip

import (
	"fmt"
	"io"
	"net"
)

func Ping(conn net.Conn) (err error) {
	request := &PingRequest{}
	var header *Header
	header, err = sendRequestReceiveHeader[*PingRequest](conn, request)
	if err != nil {
		return err
	}
	if header.MessageType != PingResponseMsgType {
		return fmt.Errorf("invalid connect response messagetype %d", header.MessageType)
	}
	return nil
}

type PingRequest struct{}

type PingResponse struct{}

func (c *PingRequest) Read(io.Reader) error {
	return nil
}

func (c *PingRequest) Write(io.Writer) error {
	return nil
}

func (c *PingRequest) MessageType() uint32 {
	return PingRequestMsgType
}

func (c *PingResponse) Read(io.Reader) error {
	return nil
}

func (c *PingResponse) Write(io.Writer) error {
	return nil
}

func (c *PingResponse) MessageType() uint32 {
	return PingResponseMsgType
}
