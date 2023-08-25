package sip

import (
	"encoding/binary"
	"io"
)

type Header struct {
	TransactionID uint32
	MessageType   uint32
}

func (h *Header) Read(reader io.Reader) error {
	return binary.Read(reader, binary.LittleEndian, h)
}

func (h *Header) Write(writer io.Writer) error {
	return binary.Write(writer, binary.LittleEndian, *h)
}
