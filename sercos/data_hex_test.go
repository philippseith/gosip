package sercos

import (
	"reflect"
	"testing"
)

func TestDataHex(t *testing.T) {
	tests := []struct {
		name   string
		data   Data
		length int
		want   string
	}{
		{name: "single byte", data: Data{0xA5}, length: 1, want: "0xA5"},
		{name: "little endian", data: Data{0x01, 0x02}, length: 2, want: "0x0201"},
		{name: "zero padding", data: Data{0xA5}, length: 2, want: "0x00A5"},
		{name: "truncate excess", data: Data{0x01, 0x02}, length: 1, want: "0x01"},
		{name: "invalid length", data: Data{0x01}, length: 3, want: "invalid hex length 3"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.data.Hex(test.length); got != test.want {
				t.Errorf("Hex(%d) = %q, want %q", test.length, got, test.want)
			}
		})
	}
}

func TestDataHexen(t *testing.T) {
	data := Data{0x01, 0x02, 0x03, 0x04}
	got := data.Hexen(2)
	want := []string{"0x0201", "0x0403"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Hexen(2) = %v, want %v", got, want)
	}

	if got := (Data{0x01, 0x02, 0x03}).Hexen(2); len(got) != 1 || got[0] != "invalid length 3 for 2-byte elements" {
		t.Errorf("Hexen(2) = %v, want an invalid length message for non-divisible data length", got)
	}
	if got := data.Hexen(3); len(got) != 1 || got[0] != "invalid hex length 3" {
		t.Errorf("Hexen(3) = %v, want invalid length message", got)
	}
}