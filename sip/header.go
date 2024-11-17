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
	return errorx.EnsureStackTrace(binary.Read(reader, binary.LittleEndian, h))
}

func (h *Header) Write(writer io.Writer) error {
	return errorx.EnsureStackTrace(binary.Write(writer, binary.LittleEndian, *h))
}
