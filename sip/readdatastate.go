package sip

import (
	"encoding/binary"
	"io"

	"github.com/joomcode/errorx"
)

type ReadDataStateRequest Request

func (r *ReadDataStateRequest) Init(slaveIndex, slaveExtension int, idn uint32) {
	r.SlaveIndex = uint16(slaveIndex)         // nolint:gosec
	r.SlaveExtension = uint16(slaveExtension) // nolint:gosec
	r.IDN = idn
}

func (r *ReadDataStateRequest) Read(reader io.Reader) error {
	if err := binary.Read(reader, binary.LittleEndian, r); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadDataStateRequest) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, *r); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadDataStateRequest) MessageType() MessageType {
	return ReadDataStateRequestMsgType
}

func (r *ReadDataStateRequest) Target() Request {
	return Request{
		r.SlaveIndex,
		r.SlaveExtension,
		r.IDN,
	}
}

type ReadDataStateResponse struct {
	DataState uint16
}

func (r *ReadDataStateResponse) Read(reader io.Reader) error {
	if err := binary.Read(reader, binary.LittleEndian, r); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadDataStateResponse) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, *r); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadDataStateResponse) MessageType() MessageType {
	return ReadDataStateResponseMsgType
}

func newReadDataStatePDUs(slaveIndex, slaveExtension int, idn uint32) (*ReadDataStateRequest, *ReadDataStateResponse) {
	return &ReadDataStateRequest{
		SlaveIndex:     uint16(slaveIndex),     // nolint:gosec
		SlaveExtension: uint16(slaveExtension), // nolint:gosec
		IDN:            idn,
	}, &ReadDataStateResponse{}
}
