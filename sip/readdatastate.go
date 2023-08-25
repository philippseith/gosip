package sip

import (
	"encoding/binary"
	"io"
	"net"
)

func ReadDataState(conn net.Conn, slaveIndex, slaveExtension int, idn uint32) (response ReadDataStateResponse, ex Exception, err error) {
	request := &ReadDataStateRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	}
	var header *Header
	header, err = sendRequestReceiveHeader(conn, request)
	ex, err = parseHeaderAndResponse(conn, header, err, &response)
	return response, ex, err
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
