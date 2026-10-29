package sercos

import (
	"reflect"
	"testing"
)

func TestDataUnsigned(t *testing.T) {
	tests := []struct {
		name          string
		data          Data
		length        int
		decimalPlaces int
		factor        int
		want          string
	}{
		{name: "insert decimal", data: Data{0x85}, length: 1, decimalPlaces: 2, factor: 1, want: "1.33"},
		{name: "short value pads trailing zeros", data: Data{0x0C}, length: 1, decimalPlaces: 4, factor: 1, want: "0.1200"},
		{name: "apply factor", data: Data{0x7B}, length: 1, decimalPlaces: 2, factor: 2, want: "2.46"},
		{name: "short input is zero padded", data: Data{0x0C}, length: 2, decimalPlaces: 3, factor: 1, want: "0.120"},
		{name: "excess input is ignored", data: Data{0x7B, 0x01}, length: 1, decimalPlaces: 2, factor: 1, want: "1.23"},
		{name: "empty input", data: nil, length: 4, decimalPlaces: 3, factor: 1, want: "0.000"},
		{name: "no decimal places", data: Data{0x85}, length: 1, decimalPlaces: 0, factor: 1, want: "133"},
		{name: "preserve uint64 factor product", data: Data{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, length: 8, decimalPlaces: 2, factor: 2, want: "368934881474191032.30"},
		{name: "invalid length", data: Data{0x01}, length: 3, decimalPlaces: 0, factor: 1, want: "invalid unsigned length 3"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.data.Unsigned(test.length, test.decimalPlaces, test.factor); got != test.want {
				t.Errorf("Unsigned(%d, %d, %d) = %q, want %q", test.length, test.decimalPlaces, test.factor, got, test.want)
			}
		})
	}
}

func TestDataStringUnsigned(t *testing.T) {
	attr := NewAttribute(
		WithLength(1),
		WithFormat(Unsigned),
		WithDecimalPlaces(2),
		WithConversionFactor(2),
	)
	if got := (Data{0x7B}).String(attr); got != "2.46" {
		t.Errorf("String() = %q, want %q", got, "2.46")
	}
}

func TestDataUnsigneds(t *testing.T) {
	data := Data{0x85, 0x7B}
	got := data.Unsigneds(1, 2, 1)
	want := []string{"1.33", "1.23"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Unsigneds() = %v, want %v", got, want)
	}

	if got := (Data{0x01, 0x02, 0x03}).Unsigneds(2, 0, 1); len(got) != 1 || got[0] != "invalid length 3 for 2-byte elements" {
		t.Errorf("Unsigneds() = %v, want an invalid length message for non-divisible data length", got)
	}
	if got := (Data{0x05}).Unsigned(1, 0, 0); got != "invalid conversion factor 0" {
		t.Errorf("Unsigned() with factor 0 = %q, want an invalid conversion factor message", got)
	}
	if got := (Data{0x05}).Unsigneds(1, 0, 0); len(got) != 1 || got[0] != "invalid conversion factor 0" {
		t.Errorf("Unsigneds() with factor 0 = %v, want an invalid conversion factor message", got)
	}
	if got := data.Unsigneds(3, 0, 1); len(got) != 1 || got[0] != "invalid unsigned length 3" {
		t.Errorf("Unsigneds() = %v, want invalid length message", got)
	}

	attr := NewAttribute(WithLength(1), WithFormat(Unsigned), WithDecimalPlaces(2))
	if got := data.Strings(attr); !reflect.DeepEqual(got, want) {
		t.Errorf("Strings() for unsigned format = %v, want %v", got, want)
	}
}
