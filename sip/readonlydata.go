package sip

import (
	"encoding/binary"
	"io"

	"github.com/joomcode/errorx"
)

type ReadOnlyDataRequest Request

func (r *ReadOnlyDataRequest) Init(slaveIndex, slaveExtension int, idn uint32) {
	r.SlaveIndex = uint16(slaveIndex)
	r.SlaveExtension = uint16(slaveExtension)
	r.IDN = idn
}

func (r *ReadOnlyDataRequest) Read(reader io.Reader) error {
	if err := binary.Read(reader, binary.LittleEndian, r); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadOnlyDataRequest) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, *r); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadOnlyDataRequest) MessageType() MessageType {
	return ReadOnlyDataRequestMsgType
}

type ReadOnlyDataResponse struct {
	readOnlyDataResponse
	Data []byte
}

type readOnlyDataResponse struct {
	Attribute  uint32
	DataLength uint32
}

func (r *ReadOnlyDataResponse) Read(reader io.Reader) error {
	err := binary.Read(reader, binary.LittleEndian, &r.readOnlyDataResponse)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	r.Data = make([]byte, r.DataLength)
	err = binary.Read(reader, binary.LittleEndian, r.Data)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadOnlyDataResponse) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, r.readOnlyDataResponse)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if r.DataLength > 0 {
		if err = binary.Write(writer, binary.LittleEndian, r.Data); err != nil {
			return errorx.EnsureStackTrace(err)
		}
	}
	return nil
}

func (r *ReadOnlyDataResponse) MessageType() MessageType {
	return ReadOnlyDataResponseMsgType
}

func newReadOnlyDataPDUs(slaveIndex, slaveExtension int, idn uint32) (*ReadOnlyDataRequest, *ReadOnlyDataResponse) {
	return &ReadOnlyDataRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	}, &ReadOnlyDataResponse{}
}
