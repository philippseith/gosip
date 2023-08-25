package sip

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

func ReadDataState(conn net.Conn, slaveIndex, slaveExtension int, idn uint32) (response ReadDataStateResponse, err error) {
	request := &ReadDataStateRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	}
	var header *Header
	header, err = sendRequestReceiveHeader(conn, request)
	if err != nil {
		return response, err
	}
	if header.MessageType != ReadDataStateResponseMsgType {
		return response, fmt.Errorf("invalid connect response messagetype %d", header.MessageType)
	}
	err = response.Read(conn)
	return response, err
}

type ReadDataStateRequest struct {
	SlaveIndex     uint16
	SlaveExtension uint16
	IDN            uint32
}

func (r *ReadDataStateRequest) Read(reader io.Reader) error {
	return binary.Read(reader, binary.LittleEndian, r)
}

func (r *ReadDataStateRequest) Write(writer io.Writer) error {
	return binary.Write(writer, binary.LittleEndian, *r)
}

func (r *ReadDataStateRequest) MessageType() uint32 {
	return ReadDataStateRequestMsgType
}

type ReadDataStateResponse struct {
	DataState uint16
}

func (r *ReadDataStateResponse) Read(reader io.Reader) error {
	return binary.Read(reader, binary.LittleEndian, &r)
}

func (r *ReadDataStateResponse) Write(writer io.Writer) error {
	return binary.Write(writer, binary.LittleEndian, r)
}

func (r *ReadDataStateResponse) MessageType() uint32 {
	return ReadDataStateResponseMsgType
}
