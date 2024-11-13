package sip

import (
	"encoding/binary"
	"io"
)

type Header struct {
	TransactionID uint32
	MessageType   MessageType
}

func (h *Header) Read(reader io.Reader) error {
	return errorx.Wrap(binary.Read(reader, binary.LittleEndian, h))
}

func (h *Header) Write(writer io.Writer) error {
	return errorx.Wrap(binary.Write(writer, binary.LittleEndian, *h))
}
