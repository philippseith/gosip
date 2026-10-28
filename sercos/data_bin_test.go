package sercos

import (
	"reflect"
	"testing"
)

func TestDataBin(t *testing.T) {
	tests := []struct {
		name   string
		data   Data
		length int
		want   string
	}{
		{name: "single byte", data: Data{0xA5}, length: 1, want: "0b1010.0101"},
		{name: "little endian", data: Data{0x01, 0x02}, length: 2, want: "0b0000.0010.0000.0001"},
		{name: "zero padding", data: Data{0xA5}, length: 2, want: "0b0000.0000.1010.0101"},
		{name: "truncate excess", data: Data{0x01, 0x02}, length: 1, want: "0b0000.0001"},
		{name: "invalid length", data: Data{0x01}, length: 3, want: "invalid bin length 3"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.data.Bin(test.length); got != test.want {
				t.Errorf("Bin(%d) = %q, want %q", test.length, got, test.want)
			}
		})
	}
}

func TestDataBins(t *testing.T) {
	data := Data{0x01, 0x02, 0x03, 0x04}
	got := data.Bins(2)
	want := []string{"0b0000.0010.0000.0001", "0b0000.0100.0000.0011"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Bins(2) = %v, want %v", got, want)
	}

	if got := (Data{0x01, 0x02, 0x03}).Bins(2); got != nil {
		t.Errorf("Bins(2) = %v, want nil for non-divisible data length", got)
	}
	if got := data.Bins(3); len(got) != 1 || got[0] != "invalid bin length 3" {
		t.Errorf("Bins(3) = %v, want invalid length message", got)
	}
}