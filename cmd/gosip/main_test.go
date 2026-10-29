package main

import "testing"

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
