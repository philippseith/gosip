package sip

import (
	"encoding/binary"
	"io"

	"github.com/joomcode/errorx"
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
		return errorx.EnsureStackTrace(err)
	}
	w.Data = make([]byte, w.DataLength)
	if err = binary.Read(reader, binary.LittleEndian, w.Data); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (w *WriteDataRequest) Write(writer io.Writer) error {
	err := binary.Write(writer, binary.LittleEndian, w.writeDataRequest)
	if err != nil {
		return errorx.EnsureStackTrace(err)
	}
	if w.DataLength > 0 {
		if err = binary.Write(writer, binary.LittleEndian, w.Data); err != nil {
			return errorx.EnsureStackTrace(err)
		}
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
