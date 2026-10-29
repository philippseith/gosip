package main

import (
	"bytes"
	"testing"

	"github.com/philippseith/gosip/sercos"
	"github.com/philippseith/gosip/sip"
)

func TestParseGlobalFlagsAddressDefaultPort(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{name: "IPv4", address: "192.168.112.67", want: "192.168.112.67:35021"},
		{name: "hostname", address: "device", want: "device:35021"},
		{name: "explicit port", address: "device:1234", want: "device:1234"},
		{name: "IPv6", address: "2001:db8::1", want: "[2001:db8::1]:35021"},
		{name: "bracketed IPv6", address: "[2001:db8::1]", want: "[2001:db8::1]:35021"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _, err := parseGlobalFlags([]string{"--address", tt.address, "ping"})
			if err != nil {
				t.Fatal(err)
			}
			if g.address != tt.want {
				t.Errorf("address = %q, want %q", g.address, tt.want)
			}
		})
	}
}

func TestDoSetIPMissingArgsDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("doSetIP panicked: %v", r)
		}
	}()

	if err := doSetIP(nil); err == nil {
		t.Fatal("expected an error for missing required setip arguments")
	}
}

func TestFormatLimit(t *testing.T) {
	tests := []struct {
		name      string
		value     [8]byte
		attribute sercos.Attribute
		want      string
	}{
		{
			name:      "unsigned 16-bit",
			value:     [8]byte{0xe8, 0x03},
			attribute: 0x00110001,
			want:      "1000",
		},
		{
			name:      "unsigned with three decimals",
			value:     [8]byte{0xe8, 0x03},
			attribute: 0x03110001,
			want:      "1.000",
		},
		{
			name:      "signed 16-bit ignores padding",
			value:     [8]byte{0x18, 0xfc, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			attribute: 0x00210001,
			want:      "-1000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatLimit(tt.value, tt.attribute); got != tt.want {
				t.Errorf("formatLimit() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintMinMaxOnlyValidElements(t *testing.T) {
	var buf bytes.Buffer
	printMinMax(&buf, 80, [8]byte{0xe8, 0x03}, [8]byte{0xff, 0xff}, sip.ElmMin, 0x00110001)
	const want = "Min:           1000\nMin (raw):     e8 03 00 00 00 00 00 00\n"
	if got := buf.String(); got != want {
		t.Errorf("printMinMax() = %q, want %q", got, want)
	}
}

func TestWriteRaw(t *testing.T) {
	data := make([]byte, 40)
	for i := range data {
		data[i] = byte(i)
	}
	tests := []struct {
		name  string
		width int
		label string
		data  []byte
		want  string
	}{
		{
			name:  "single line",
			width: 80,
			label: "Data (raw)",
			data:  []byte{0x02, 0x00},
			want:  "Data (raw):    02 00\n",
		},
		{
			name:  "multiple rows with offsets",
			width: 70,
			label: "Data (raw)",
			data:  data,
			want: `Data (raw):
0000  00 01 02 03 04 05 06 07 08 09 0a 0b 0c 0d 0e 0f
0010  10 11 12 13 14 15 16 17 18 19 1a 1b 1c 1d 1e 1f
0020  20 21 22 23 24 25 26 27
`,
		},
		{
			name:  "one row without offset",
			width: 20,
			label: "Data (raw)",
			data:  []byte{1, 2, 3},
			want:  "Data (raw):\n01 02 03\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeRaw(&buf, tt.width, tt.label, tt.data)
			if got := buf.String(); got != tt.want {
				t.Errorf("writeRaw() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteElements(t *testing.T) {
	tests := []struct {
		name  string
		width int
		elems []string
		want  string
	}{
		{
			name:  "single line",
			width: 80,
			elems: []string{"0b0000.0000.0000.0010"},
			want:  "Data:          0b0000.0000.0000.0010\n",
		},
		{
			name:  "multiple rows with decimal offsets",
			width: 25,
			elems: []string{"1", "22", "333", "4", "5", "6"},
			want: `Data:
0    1   22  333    4
4    5    6
`,
		},
		{
			name:  "one oversized cell without offset",
			width: 10,
			elems: []string{"abcdefghijkl"},
			want:  "Data:\nabcdefghijkl\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeElements(&buf, tt.width, "Data", tt.elems)
			if got := buf.String(); got != tt.want {
				t.Errorf("writeElements() = %q, want %q", got, tt.want)
			}
		})
	}
}
