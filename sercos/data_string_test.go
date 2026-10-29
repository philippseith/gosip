package sercos

import (
	"reflect"
	"testing"
)

func TestDataString(t *testing.T) {
	tests := []struct {
		name string
		data Data
		attr Attribute
		want string
	}{
		{
			name: "binary",
			data: Data{0x01, 0x02},
			attr: NewAttribute(WithLength(2), WithFormat(Bin)),
			want: "0b0000.0010.0000.0001",
		},
		{
			name: "hex",
			data: Data{0x01, 0x02},
			attr: NewAttribute(WithLength(2), WithFormat(Hex)),
			want: "0x0201",
		},
		{
			name: "text",
			data: Data("text"),
			attr: NewAttribute(WithFormat(Text)),
			want: "text",
		},
		{
			name: "idn length two",
			data: Data{0x01, 0x80},
			attr: NewAttribute(WithLength(2), WithFormat(IDN)),
			want: "P-0-0001",
		},
		{
			name: "idn invalid length",
			data: Data{0x01, 0x80},
			attr: NewAttribute(WithLength(1), WithFormat(IDN)),
			want: "invalid idn length 1",
		},
		{
			name: "float",
			data: Data{0x00, 0x00, 0xC0, 0x3F},
			attr: NewAttribute(WithLength(4), WithFormat(Float)),
			want: "1.5",
		},
		{
			name: "time",
			data: Data{0, 0, 0, 0, 1, 0, 0, 0},
			attr: NewAttribute(WithLength(4), WithFormat(Time)),
			want: "1970-01-01T00:00:01.000000000Z",
		},
		{
			name: "reserved bit 23 is ignored",
			data: Data{0x01, 0x00},
			attr: Attribute(0x00910001),
			want: "1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.data.String(test.attr); got != test.want {
				t.Errorf("String() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDataStrings(t *testing.T) {
	tests := []struct {
		name string
		data Data
		attr Attribute
		want []string
	}{
		{
			name: "binary list",
			data: Data{0x01, 0x02, 0x03, 0x04},
			attr: NewAttribute(WithLength(2), WithFormat(Bin), WithList(true)),
			want: []string{"0b0000.0010.0000.0001", "0b0000.0100.0000.0011"},
		},
		{
			name: "hex list",
			data: Data{0x01, 0x02, 0x03, 0x04},
			attr: NewAttribute(WithLength(2), WithFormat(Hex)),
			want: []string{"0x0201", "0x0403"},
		},
		{
			name: "text as one value",
			data: Data("text"),
			attr: NewAttribute(WithLength(2), WithFormat(Text), WithList(true)),
			want: []string{"text"},
		},
		{
			name: "idn list",
			data: Data{0x01, 0x80, 0x02, 0x10},
			attr: NewAttribute(WithLength(2), WithFormat(IDN)),
			want: []string{"P-0-0001", "S-1-0002"},
		},
		{
			name: "idn invalid length",
			data: Data{0x01, 0x80},
			attr: NewAttribute(WithLength(1), WithFormat(IDN)),
			want: []string{"invalid idn length 1"},
		},
		{
			name: "float list",
			data: Data{0x00, 0x00, 0xC0, 0x3F, 0x00, 0x00, 0x20, 0xC0},
			attr: NewAttribute(WithLength(4), WithFormat(Float)),
			want: []string{"1.5", "-2.5"},
		},
		{
			name: "time list",
			data: Data{
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 1, 0, 0, 0,
			},
			attr: NewAttribute(WithLength(8), WithFormat(Time)),
			want: []string{
				"1970-01-01T00:00:00.000000000Z",
				"1970-01-01T00:00:01.000000000Z",
			},
		},
		{
			name: "reserved bit 23 is ignored",
			data: Data{0x01, 0x00},
			attr: Attribute(0x00910001),
			want: []string{"1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.data.Strings(test.attr); !reflect.DeepEqual(got, test.want) {
				t.Errorf("Strings() = %v, want %v", got, test.want)
			}
		})
	}

	data := Data{0x01, 0x02, 0x03}
	attr := NewAttribute(WithLength(2), WithFormat(Bin))
	if got := data.Strings(attr); len(got) != 1 || got[0] != "invalid length 3 for 2-byte elements" {
		t.Errorf("Strings() = %v, want an invalid length message for non-divisible data length", got)
	}
}
