package main

import (
	"io"
	"os"
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
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	printMinMax([8]byte{0xe8, 0x03}, [8]byte{0xff, 0xff}, sip.ElmMin, 0x00110001)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdout
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	const want = "Min:           1000\nMin (raw):     e803000000000000\n"
	if string(got) != want {
		t.Errorf("printMinMax() = %q, want %q", got, want)
	}
}
