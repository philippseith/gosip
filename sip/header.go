package sip

import (
	"encoding/binary"
	"io"

	"github.com/joomcode/errorx"
)

type Header struct {
	TransactionID uint32
	MessageType   MessageType
}

func (h *Header) Read(reader io.Reader) error {
	if err := binary.Read(reader, binary.LittleEndian, h); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (h *Header) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, *h); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}
