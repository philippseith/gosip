package sip

import (
	"io"
	"net"
)

func Ping(conn net.Conn) (ex Exception, err error) {
	request := &PingRequest{}
	var header *Header
	header, err = sendRequestReceiveHeader(conn, request)
	var response PingResponse
	ex, err = parseHeaderAndResponse(conn, header, err, &response)
	return ex, err
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
