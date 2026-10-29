package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/philippseith/gosip/sercos"
	"github.com/philippseith/gosip/sip"
)

// dataClient is the part of sip.Client used by the write commands.
type dataClient interface {
	ReadOnlyData(slaveIndex, slaveExtension int, idn uint32, options ...sip.RequestOption) (sip.ReadOnlyDataResponse, error)
	WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte, options ...sip.RequestOption) error
}

func splitElements(attr sercos.Attribute, input string) []string {
	if attr.Format() == sercos.Text {
		return []string{strings.TrimSpace(input)}
	}
	return strings.Fields(input)
}

func encodeFormattedData(attr sercos.Attribute, input string) ([]byte, error) {
	data, err := sercos.ParseDatas(attr, splitElements(attr, input))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("no data to write")
	}
	return data, nil
}

func decodeRawData(input string) ([]byte, error) {
	var data []byte
	for _, part := range strings.Fields(input) {
		decoded, err := hex.DecodeString(strings.TrimPrefix(part, "0x"))
		if err != nil {
			return nil, fmt.Errorf("invalid raw data: %w", err)
		}
		data = append(data, decoded...)
	}
	if len(data) == 0 {
		return nil, errors.New("no data to write")
	}
	return data, nil
}

func checkWriteLength(attr sercos.Attribute, currentLen int, data []byte) error {
	if attr.Format() == sercos.Text {
		return nil
	}
	if attr.IsList() {
		elementLen := attr.Length()
		if attr.Format() == sercos.Time {
			elementLen = 8
		}
		if len(data)%elementLen != 0 {
			return fmt.Errorf("data length %d is not a multiple of the element length %d", len(data), elementLen)
		}
		return nil
	}
	if len(data) != currentLen {
		return fmt.Errorf("data length %d does not match the IDN data length %d", len(data), currentLen)
	}
	return nil
}

func writeData(client dataClient, out io.Writer, slaveIndex, slaveExtension int, idn uint32, data []byte, options []sip.RequestOption) error {
	if err := client.WriteData(slaveIndex, slaveExtension, idn, data, options...); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "wrote %d bytes to %s\n", len(data), sercos.Idn(idn))
	return err
}

func writeIDN(client dataClient, out io.Writer, slaveIndex, slaveExtension int, idn uint32, input string, raw bool, options []sip.RequestOption) error {
	current, err := client.ReadOnlyData(slaveIndex, slaveExtension, idn, options...)
	if err != nil {
		return err
	}
	attr := sercos.Attribute(current.Attribute)
	var data []byte
	if raw {
		data, err = decodeRawData(input)
	} else {
		data, err = encodeFormattedData(attr, input)
	}
	if err != nil {
		return err
	}
	if err := checkWriteLength(attr, len(current.Data), data); err != nil {
		return err
	}
	return writeData(client, out, slaveIndex, slaveExtension, idn, data, options)
}
