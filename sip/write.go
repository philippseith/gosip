package sip

import (
	"encoding/binary"
	"io"
)

type WriteDataRequest struct {
	writeDataRequest
	Data []byte
}
type writeDataRequest struct {
	Request
	DataLength uint32
}

func (w *WriteDataRequest) Read(reader io.Reader) error {
	err := binary.Read(reader, binary.LittleEndian, &w.writeDataRequest)
	if err != nil {
		return errorx.Wrap(err)
	}
	w.Data = make([]byte, w.DataLength)
	return errorx.Wrap(binary.Read(reader, binary.LittleEndian, w.Data))
}

func (w *WriteDataRequest) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, w.writeDataRequest)
	if err != nil {
		return errorx.Wrap(err)
	}
	if w.DataLength > 0 {
		return errorx.Wrap(binary.Write(writer, binary.LittleEndian, w.Data))
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
