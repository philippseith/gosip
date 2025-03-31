package sip

import (
	"encoding/binary"
	"io"

	"github.com/joomcode/errorx"
)

type ReadDescriptionRequest Request

func (r *ReadDescriptionRequest) Init(slaveIndex, slaveExtension int, idn uint32) {
	r.SlaveIndex = uint16(slaveIndex)         // nolint:gosec
	r.SlaveExtension = uint16(slaveExtension) // nolint:gosec
	r.IDN = idn
}

func (r *ReadDescriptionRequest) Read(reader io.Reader) error {
	if err := binary.Read(reader, binary.LittleEndian, r); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadDescriptionRequest) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, *r); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadDescriptionRequest) MessageType() MessageType {
	return ReadDescriptionRequestMsgType
}

func (r *ReadDescriptionRequest) Target() Request {
	return Request{
		r.SlaveIndex,
		r.SlaveExtension,
		r.IDN,
	}
}

type ReadDescriptionResponse struct {
	readDescriptionResponse
	Name []byte
	Unit []byte
}

type readDescriptionResponse struct {
	ValidElements uint16
	NameLength    uint16
	Attribute     uint32
	UnitLength    uint16
	Min           [8]byte
	Max           [8]byte
	MaxListLength uint32
}

func (r *ReadDescriptionResponse) Read(reader io.Reader) error {
	err := binary.Read(reader, binary.LittleEndian, &r.readDescriptionResponse)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	r.Name = make([]byte, r.NameLength)
	err = binary.Read(reader, binary.LittleEndian, r.Name)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	r.Unit = make([]byte, r.UnitLength)
	if err = binary.Read(reader, binary.LittleEndian, r.Unit); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (r *ReadDescriptionResponse) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, r.readDescriptionResponse)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if r.NameLength > 0 {
		err = binary.Write(writer, binary.LittleEndian, r.Name)
		if err != nil {
			return errorx.EnsureStackTrace(err)
		}
	}
	if r.UnitLength > 0 {
		if err = binary.Write(writer, binary.LittleEndian, r.Unit); err != nil {
			return errorx.EnsureStackTrace(err)
		}
	}
	return nil
}

func (r *ReadDescriptionResponse) MessageType() MessageType {
	return ReadDescriptionResponseMsgType
}

func newReadDescriptionPDUs(slaveIndex, slaveExtension int, idn uint32) (*ReadDescriptionRequest, *ReadDescriptionResponse) {
	return &ReadDescriptionRequest{
		SlaveIndex:     uint16(slaveIndex),     // nolint:gosec
		SlaveExtension: uint16(slaveExtension), // nolint:gosec
		IDN:            idn,
	}, &ReadDescriptionResponse{}
}
