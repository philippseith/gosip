package sip

import (
	"encoding/binary"
	"io"
)

type ReadEverythingRequest struct {
	SlaveIndex     uint16
	SlaveExtension uint16
	IDN            uint32
}

func (r *ReadEverythingRequest) Read(reader io.Reader) error {
	return binary.Read(reader, binary.LittleEndian, r)
}

func (r *ReadEverythingRequest) Write(writer io.Writer) error {
	return binary.Write(writer, binary.LittleEndian, *r)
}

func (r *ReadEverythingRequest) MessageType() MessageType {
	return ReadEverythingRequestMsgType
}

type ReadEverythingResponse struct {
	readEverythingResponse
	Name []byte
	Unit []byte
	Data []byte
}

type readEverythingResponse struct {
	ValidElements uint16
	DataState     uint16
	NameLength    uint16
	Attribute     uint32
	UnitLength    uint16
	Min           [8]byte
	Max           [8]byte
	MaxListLength uint32
	DataLength    uint32
}

func (r *ReadEverythingResponse) Read(reader io.Reader) error {
	err := binary.Read(reader, binary.LittleEndian, &r.readEverythingResponse)
	if err != nil {
		return err
	}
	r.Name = make([]byte, r.NameLength)
	err = binary.Read(reader, binary.LittleEndian, r.Name)
	if err != nil {
		return err
	}
	r.Unit = make([]byte, r.UnitLength)
	err = binary.Read(reader, binary.LittleEndian, r.Unit)
	if err != nil {
		return err
	}
	r.Data = make([]byte, r.DataLength)
	return binary.Read(reader, binary.LittleEndian, r.Data)
}

func (r *ReadEverythingResponse) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, r.readEverythingResponse)
	if err != nil {
		return err
	}
	if r.NameLength > 0 {
		err = binary.Write(writer, binary.LittleEndian, r.Name)
		if err != nil {
			return err
		}
	}
	if r.UnitLength > 0 {
		err = binary.Write(writer, binary.LittleEndian, r.Unit)
		if err != nil {
			return err
		}
	}
	if r.DataLength > 0 {
		return binary.Write(writer, binary.LittleEndian, r.Data)
	}
	return nil
}

func (r *ReadEverythingResponse) MessageType() MessageType {
	return ReadEverythingResponseMsgType
}
