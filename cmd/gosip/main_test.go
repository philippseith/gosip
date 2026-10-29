package main

import (
	"bytes"
	"strings"
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

func TestParseIdn(t *testing.T) {
	tests := []struct {
		input   string
		want    uint32
		wantErr string
	}{
		{input: "S-0-0032.0.0", want: 32},
		{input: "S-0-0032", want: 32},
		{input: "0x100A", want: 0x100A},
		{input: "7", want: 7},
		{input: "0095", want: 95},
		{input: "0010", want: 10},
		{input: "S-95", want: 95},
		{input: "S-0-95", want: 95},
		{input: "P-100", want: 0x8064},
		{input: "p-1-2", want: 0x9002},
		{input: "1-0010", want: 0x100A},
		{input: "4106", want: 0x100A},
		{input: "S-1-10.1.2", want: 0x0102100A},
		{input: " P-0-0100.1.2 ", want: 0x01028064},
		{input: "s-1-0002", want: 0x1002},
		{input: "S-7-4095.255.255", want: 0xFFFF7FFF},
		{input: "S-8-0001", wantErr: "invalid IDN"},
		{input: "S-0-4096", wantErr: "invalid IDN"},
		{input: "X-0-0001", wantErr: "invalid IDN"},
		{input: "S-0-0001.1", wantErr: "invalid IDN"},
		{input: "S-0-0001.256.0", wantErr: "invalid IDN"},
		{input: "S-1-2-3", wantErr: "invalid IDN"},
		{input: "S-12-5", wantErr: "invalid IDN"},
		{input: "0x", wantErr: "invalid IDN"},
		{input: "not-an-idn", wantErr: "invalid IDN"},
		{input: "", wantErr: "invalid IDN"},
		{input: "4294967296", wantErr: "invalid IDN"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseIdn(tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseIdn(%q) error = %v, want substring %q", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseIdn(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseIdn(%q) = %#x, want %#x", tt.input, got, tt.want)
			}
		})
	}
}
