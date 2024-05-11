package sip

import (
	"encoding/binary"
	"io"

	"braces.dev/errtrace"
)

type Header struct {
	TransactionID uint32
	MessageType   MessageType
}

func (h *Header) Read(reader io.Reader) error {
	return errtrace.Wrap(binary.Read(reader, binary.LittleEndian, h))
}

func (h *Header) Write(writer io.Writer) error {
	return errtrace.Wrap(binary.Write(writer, binary.LittleEndian, *h))
}
