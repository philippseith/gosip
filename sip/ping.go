package sip

import (
	"io"
)

type PingRequest struct{}

type PingResponse struct{}

func (c *PingRequest) Read(io.Reader) error {
	return nil
}

func (c *PingRequest) Write(io.Writer) error {
	return nil
}

func (c *PingRequest) MessageType() MessageType {
	return PingRequestMsgType
}

func (c *PingResponse) Read(io.Reader) error {
	return nil
}

func (c *PingResponse) Write(io.Writer) error {
	return nil
}

func (c *PingResponse) MessageType() MessageType {
	return PingResponseMsgType
}
