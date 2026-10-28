package sercos

import (
	"reflect"
	"testing"
)

func TestDataIdn(t *testing.T) {
	tests := []struct {
		name   string
		data   Data
		length int
		want   string
	}{
		{name: "idn s2", data: Data{0x01, 0x80}, length: 2, want: "P-0-0001"},
		{name: "idn", data: Data{0x02, 0x10, 0x02, 0x03}, length: 4, want: "S-1-0002.3.2"},
		{name: "zero padding", data: Data{0x01}, length: 2, want: "S-0-0001"},
		{name: "truncate excess", data: Data{0x02, 0x10, 0xFF}, length: 2, want: "S-1-0002"},
		{name: "invalid length", data: Data{0x01}, length: 3, want: "invalid idn length 3"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.data.Idn(test.length); got != test.want {
				t.Errorf("Idn(%d) = %q, want %q", test.length, got, test.want)
			}
		})
	}
}

func TestDataIdns(t *testing.T) {
	data := Data{0x01, 0x80, 0x02, 0x10}
	got := data.Idns(2)
	want := []string{"P-0-0001", "S-1-0002"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Idns(2) = %v, want %v", got, want)
	}

	if got := (Data{0x01, 0x80, 0x02}).Idns(2); got != nil {
		t.Errorf("Idns(2) = %v, want nil for non-divisible data length", got)
	}
	if got := data.Idns(3); len(got) != 1 || got[0] != "invalid idn length 3" {
		t.Errorf("Idns(3) = %v, want invalid length message", got)
	}
}