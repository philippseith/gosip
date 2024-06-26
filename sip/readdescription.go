package sip

import (
	"encoding/binary"
	"io"

	"braces.dev/errtrace"
)

type ReadDescriptionRequest Request

func (r *ReadDescriptionRequest) Init(slaveIndex, slaveExtension int, idn uint32) {
	r.SlaveIndex = uint16(slaveIndex)
	r.SlaveExtension = uint16(slaveExtension)
	r.IDN = idn
}

func (r *ReadDescriptionRequest) Read(reader io.Reader) error {
	return errtrace.Wrap(binary.Read(reader, binary.LittleEndian, r))
}

func (r *ReadDescriptionRequest) Write(writer io.Writer) error {
	return errtrace.Wrap(binary.Write(writer, binary.LittleEndian, *r))
}

func (r *ReadDescriptionRequest) MessageType() MessageType {
	return ReadDescriptionRequestMsgType
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
		return errtrace.Wrap(err)
	}
	r.Name = make([]byte, r.NameLength)
	err = binary.Read(reader, binary.LittleEndian, r.Name)
	if err != nil {
		return errtrace.Wrap(err)
	}
	r.Unit = make([]byte, r.UnitLength)
	return errtrace.Wrap(binary.Read(reader, binary.LittleEndian, r.Unit))
}

func (r *ReadDescriptionResponse) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, r.readDescriptionResponse)
	if err != nil {
		return errtrace.Wrap(err)
	}
	if r.NameLength > 0 {
		err = binary.Write(writer, binary.LittleEndian, r.Name)
		if err != nil {
			return errtrace.Wrap(err)
		}
	}
	if r.UnitLength > 0 {
		return errtrace.Wrap(binary.Write(writer, binary.LittleEndian, r.Unit))
	}
	return nil
}

func (r *ReadDescriptionResponse) MessageType() MessageType {
	return ReadDescriptionResponseMsgType
}

func newReadDescriptionPDUs(slaveIndex, slaveExtension int, idn uint32) (*ReadDescriptionRequest, *ReadDescriptionResponse) {
	return &ReadDescriptionRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	}, &ReadDescriptionResponse{}
}
