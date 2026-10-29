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
		{name: "max int64 times factor does not overflow", data: Data{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x7F}, length: 8, decimalPlaces: 0, factor: 10, want: "92233720368547758070"},
		{name: "min int64 times factor does not overflow", data: Data{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80}, length: 8, decimalPlaces: 2, factor: 10, want: "-922337203685477580.80"},
		{name: "int32 times largest factor", data: Data{0xFF, 0xFF, 0xFF, 0x7F}, length: 4, decimalPlaces: 0, factor: 65535, want: "140735340806145"},
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

	if got := (Data{0x01, 0x02, 0x03}).Signeds(2, 0, 1); len(got) != 1 || got[0] != "invalid length 3 for 2-byte elements" {
		t.Errorf("Signeds() = %v, want an invalid length message for non-divisible data length", got)
	}
	if got := (Data{0x05}).Signed(1, 0, 0); got != "invalid conversion factor 0" {
		t.Errorf("Signed() with factor 0 = %q, want an invalid conversion factor message", got)
	}
	if got := (Data{0x05}).Signeds(1, 0, 0); len(got) != 1 || got[0] != "invalid conversion factor 0" {
		t.Errorf("Signeds() with factor 0 = %v, want an invalid conversion factor message", got)
	}
	if got := data.Signeds(3, 0, 1); len(got) != 1 || got[0] != "invalid signed length 3" {
		t.Errorf("Signeds() = %v, want invalid length message", got)
	}

	attr := NewAttribute(WithLength(1), WithFormat(Signed), WithDecimalPlaces(2))
	if got := data.Strings(attr); !reflect.DeepEqual(got, want) {
		t.Errorf("Strings() for signed format = %v, want %v", got, want)
	}
}
