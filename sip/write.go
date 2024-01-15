package sip

import (
	"braces.dev/errtrace"
	"encoding/binary"
	"io"
)

type WriteDataRequest struct {
	writeDataRequest
	Data []byte
}
type writeDataRequest struct {
	SlaveIndex     uint16
	SlaveExtension uint16
	IDN            uint32
	DataLength     uint32
}

func (w *WriteDataRequest) Read(reader io.Reader) error {
	err := binary.Read(reader, binary.LittleEndian, &w.writeDataRequest)
	if err != nil {
		return errtrace.Wrap(err)
	}
	w.Data = make([]byte, w.DataLength)
	return errtrace.Wrap(binary.Read(reader, binary.LittleEndian, w.Data))
}

func (w *WriteDataRequest) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, w.writeDataRequest)
	if err != nil {
		return errtrace.Wrap(err)
	}
	if w.DataLength > 0 {
		return errtrace.Wrap(binary.Write(writer, binary.LittleEndian, w.Data))
	}
	return nil
}

func (w *WriteDataRequest) MessageType() MessageType {
	return WriteDataRequestMsgType
}

type WriteDataResponse struct{}

func (c *WriteDataResponse) Read(io.Reader) error {
	return nil
}

func (c *WriteDataResponse) Write(io.Writer) error {
	return nil
}

func (c *WriteDataResponse) MessageType() MessageType {
	return WriteDataResponseMsgType
}
