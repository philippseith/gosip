package sip

import (
	"encoding/binary"
	"io"
)

type ReadDataStateRequest struct {
	SlaveIndex     uint16
	SlaveExtension uint16
	IDN            uint32
}

func (r *ReadDataStateRequest) Init(slaveIndex, slaveExtension int, idn uint32) {
	r.SlaveIndex = uint16(slaveIndex)
	r.SlaveExtension = uint16(slaveExtension)
	r.IDN = idn
}

func (r *ReadDataStateRequest) Read(reader io.Reader) error {
	return binary.Read(reader, binary.LittleEndian, r)
}

func (r *ReadDataStateRequest) Write(writer io.Writer) error {
	return binary.Write(writer, binary.LittleEndian, *r)
}

func (r *ReadDataStateRequest) MessageType() MessageType {
	return ReadDataStateRequestMsgType
}

type ReadDataStateResponse struct {
	DataState uint16
}

func (r *ReadDataStateResponse) Read(reader io.Reader) error {
	return binary.Read(reader, binary.LittleEndian, r)
}

func (r *ReadDataStateResponse) Write(writer io.Writer) error {
	return binary.Write(writer, binary.LittleEndian, *r)
}

func (r *ReadDataStateResponse) MessageType() MessageType {
	return ReadDataStateResponseMsgType
}
