package sercos

import (
	"fmt"
	"testing"
	"time"
)

// FuzzDataFormatting feeds device-controlled bytes and a device-controlled
// attribute word to every display entry point. Oracle: no panic, and every call
// returns within one second.
func FuzzDataFormatting(f *testing.F) {
	f.Add(uint32(NewAttribute(WithFormat(Unsigned), WithLength(4), WithConversionFactor(1))), []byte{1, 2, 3, 4})
	f.Add(uint32(NewAttribute(WithFormat(Signed), WithLength(8), WithConversionFactor(65535), WithDecimalPlaces(15))), []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f})
	f.Add(uint32(NewAttribute(WithFormat(Float), WithLength(8))), []byte{0, 0, 0, 0, 0, 0, 0xf0, 0x7f})
	f.Add(uint32(NewAttribute(WithFormat(Time), WithLength(8))), []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f})
	f.Add(uint32(NewAttribute(WithFormat(IDN), WithLength(4), WithList(true))), []byte{1, 0, 2, 0, 3, 0, 4, 0})
	f.Add(uint32(NewAttribute(WithFormat(Text))), []byte("hello"))
	f.Add(uint32(NewAttribute(WithFormat(Bin), WithLength(2))), []byte{1, 2})
	f.Add(uint32(NewAttribute(WithFormat(Hex), WithLength(1), WithList(true))), []byte{1, 2, 3})
	f.Fuzz(func(t *testing.T, attrBits uint32, data []byte) {
		attr := Attribute(attrBits)
		d := Data(data)
		start := time.Now()
		_ = d.String(attr)
		_ = d.Strings(attr)
		for _, verb := range "buxXsifdt" {
			for _, width := range []int{1, 2, 3, 4, 8, 9, 64} {
				_ = fmt.Sprintf(fmt.Sprintf("%%%d.3%c", width, verb), d)
			}
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("formatting took %v for attribute %#x and %d bytes", elapsed, attrBits, len(data))
		}
	})
}

// FuzzParseData feeds arbitrary text and attribute words to the value parser
// (reachable from MCP writedata and the interactive prompt). Oracle: no panic,
// returns within one second, and a successfully parsed value formats without panic.
func FuzzParseData(f *testing.F) {
	f.Add(uint32(NewAttribute(WithFormat(Unsigned), WithLength(4), WithConversionFactor(1))), "42")
	f.Add(uint32(NewAttribute(WithFormat(Signed), WithLength(2), WithConversionFactor(10), WithDecimalPlaces(1))), "-1.5")
	f.Add(uint32(NewAttribute(WithFormat(Float), WithLength(8))), "1e308")
	f.Add(uint32(NewAttribute(WithFormat(Unsigned), WithLength(8), WithConversionFactor(1))), "1e1000000")
	f.Add(uint32(NewAttribute(WithFormat(Time), WithLength(8))), "2024-01-02T03:04:05Z")
	f.Add(uint32(NewAttribute(WithFormat(IDN), WithLength(4))), "S-0-0017")
	f.Add(uint32(NewAttribute(WithFormat(Hex), WithLength(2))), "0xBEEF")
	f.Fuzz(func(t *testing.T, attrBits uint32, text string) {
		attr := Attribute(attrBits)
		start := time.Now()
		d, err := ParseData(attr, text)
		if err == nil {
			_ = d.String(attr)
			_ = d.Strings(attr)
		}
		_, _ = ParseDatas(attr, []string{text, text})
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("parsing took %v for attribute %#x and %q", elapsed, attrBits, text)
		}
	})
}
