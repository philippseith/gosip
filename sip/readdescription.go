package sip

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

func ReadDescription(conn net.Conn, slaveIndex, slaveExtension int, idn uint32) (response ReadDescriptionResponse, err error) {
	request := &ReadDescriptionRequest{
		SlaveIndex:     uint16(slaveIndex),
		SlaveExtension: uint16(slaveExtension),
		IDN:            idn,
	}
	var header *Header
	header, err = sendRequestReceiveHeader(conn, request)
	if err != nil {
		return response, err
	}
	if header.MessageType != ReadDescriptionResponseMsgType {
		return response, fmt.Errorf("invalid connect response messagetype %d", header.MessageType)
	}
	err = response.Read(conn)
	return response, err
}

type ReadDescriptionRequest struct {
	SlaveIndex     uint16
	SlaveExtension uint16
	IDN            uint32
}

func (r *ReadDescriptionRequest) Read(reader io.Reader) error {
	return binary.Read(reader, binary.LittleEndian, r)
}

func (r *ReadDescriptionRequest) Write(writer io.Writer) error {
	return binary.Write(writer, binary.LittleEndian, *r)
}

func (r *ReadDescriptionRequest) MessageType() uint32 {
	return ReadDescriptionRequestMsgType
}

type ReadDescriptionResponse struct {
	readDescriptionResponse
	Name []byte
	Unit []byte
}

type readDescriptionResponse struct {
	ValidElements uint16
	DataState     uint16
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
		return err
	}
	r.Name = make([]byte, r.NameLength)
	err = binary.Read(reader, binary.LittleEndian, r.Name)
	if err != nil {
		return err
	}
	r.Unit = make([]byte, r.UnitLength)
	return binary.Read(reader, binary.LittleEndian, r.Unit)
}

func (r *ReadDescriptionResponse) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, r.readDescriptionResponse)
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
		return binary.Write(writer, binary.LittleEndian, r.Unit)
	}
	return nil
}

func (r *ReadDescriptionResponse) MessageType() uint32 {
	return ReadDescriptionResponseMsgType
}
