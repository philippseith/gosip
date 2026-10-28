package sercos

import (
	"reflect"
	"testing"
)

func TestDataSigned(t *testing.T) {
	tests := []struct {
		name          string
		data          Data
		length        int
		decimalPlaces int
		factor        int
		want          string
	}{
		{name: "insert decimal", data: Data{0x85}, length: 1, decimalPlaces: 2, factor: 1, want: "-1.23"},
		{name: "negative int16", data: Data{0x85, 0xFF}, length: 2, decimalPlaces: 2, factor: 1, want: "-1.23"},
		{name: "negative int32", data: Data{0x85, 0xFF, 0xFF, 0xFF}, length: 4, decimalPlaces: 0, factor: 1, want: "-123"},
		{name: "negative int64", data: Data{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, length: 8, decimalPlaces: 0, factor: 1, want: "-1"},
		{name: "short value pads trailing zeros", data: Data{0x0C}, length: 1, decimalPlaces: 4, factor: 1, want: "0.1200"},
		{name: "apply factor", data: Data{0x7B}, length: 1, decimalPlaces: 2, factor: 2, want: "2.46"},
		{name: "short input is zero padded", data: Data{0x0C}, length: 2, decimalPlaces: 3, factor: 1, want: "0.120"},
		{name: "excess input is ignored", data: Data{0x85, 0xFF}, length: 1, decimalPlaces: 2, factor: 1, want: "-1.23"},
		{name: "empty input", data: nil, length: 4, decimalPlaces: 3, factor: 1, want: "0.000"},
		{name: "no decimal places", data: Data{0x85}, length: 1, decimalPlaces: 0, factor: 1, want: "-123"},
		{name: "invalid length", data: Data{0x01}, length: 3, decimalPlaces: 0, factor: 1, want: "invalid signed length 3"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.data.Signed(test.length, test.decimalPlaces, test.factor); got != test.want {
				t.Errorf("Signed(%d, %d, %d) = %q, want %q", test.length, test.decimalPlaces, test.factor, got, test.want)
			}
		})
	}
}

func TestDataStringSigned(t *testing.T) {
	attr := NewAttribute(
		WithLength(1),
		WithFormat(Signed),
		WithDecimalPlaces(2),
		WithConversionFactor(2),
	)
	if got := (Data{0x85}).String(attr); got != "-2.46" {
		t.Errorf("String() = %q, want %q", got, "-2.46")
	}
}

func TestDataSigneds(t *testing.T) {
	data := Data{0x85, 0x7B}
	got := data.Signeds(1, 2, 1)
	want := []string{"-1.23", "1.23"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Signeds() = %v, want %v", got, want)
	}

	if got := (Data{0x01, 0x02, 0x03}).Signeds(2, 0, 1); got != nil {
		t.Errorf("Signeds() = %v, want nil for non-divisible data length", got)
	}
	if got := data.Signeds(3, 0, 1); len(got) != 1 || got[0] != "invalid signed length 3" {
		t.Errorf("Signeds() = %v, want invalid length message", got)
	}

	attr := NewAttribute(WithLength(1), WithFormat(Signed), WithDecimalPlaces(2))
	if got := data.Strings(attr); !reflect.DeepEqual(got, want) {
		t.Errorf("Strings() for signed format = %v, want %v", got, want)
	}
}
