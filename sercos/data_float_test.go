package sercos

import (
	"math"
	"reflect"
	"testing"
)

func TestDataFloat(t *testing.T) {
	tests := []struct {
		name   string
		data   Data
		length int
		want   string
	}{
		{name: "float32 little endian", data: Data{0x00, 0x00, 0xC0, 0x3F}, length: 4, want: "1.5"},
		{name: "float64 little endian", data: Data{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xC0}, length: 8, want: "-2.25"},
		{name: "zero padding", data: Data{0x00, 0x00, 0x80}, length: 4, want: "1.1754944E-38"},
		{name: "truncate excess", data: Data{0x00, 0x00, 0xC0, 0x3F, 0xFF}, length: 4, want: "1.5"},
		{name: "invalid length", data: Data{0x00}, length: 2, want: "invalid float length 2"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.data.Float(test.length); got != test.want {
				t.Errorf("Float(%d) = %q, want %q", test.length, got, test.want)
			}
		})
	}

	if got := (Data{}).Float(4); got != "0" {
		t.Errorf("Float(4) for empty data = %q, want %q", got, "0")
	}
}

func TestDataFloats(t *testing.T) {
	data := Data{0x00, 0x00, 0xC0, 0x3F, 0x00, 0x00, 0x20, 0xC0}
	got := data.Floats(4)
	want := []string{"1.5", "-2.5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Floats(4) = %v, want %v", got, want)
	}

	if got := (Data{0x00, 0x00, 0xC0, 0x3F, 0x00}).Floats(4); got != nil {
		t.Errorf("Floats(4) = %v, want nil for non-divisible data length", got)
	}
	if got := data.Floats(2); len(got) != 1 || got[0] != "invalid float length 2" {
		t.Errorf("Floats(2) = %v, want invalid length message", got)
	}

	doubleBits := math.Float64bits(-2.25)
	doubleData := Data{
		byte(doubleBits),
		byte(doubleBits >> 8),
		byte(doubleBits >> 16),
		byte(doubleBits >> 24),
		byte(doubleBits >> 32),
		byte(doubleBits >> 40),
		byte(doubleBits >> 48),
		byte(doubleBits >> 56),
	}
	if got := doubleData.Floats(8); !reflect.DeepEqual(got, []string{"-2.25"}) {
		t.Errorf("Floats(8) = %v, want [-2.25]", got)
	}
}
