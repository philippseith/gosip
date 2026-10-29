package sercos

import (
	"fmt"
	"reflect"
	"testing"
)

func TestDataScan(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		format  string
		initial Data
		want    Data
		wantErr bool
	}{
		{name: "binary formatted", input: "0b0000.0000.0000.0010", format: "%b", want: Data{0x02, 0x00}},
		{name: "binary without prefix", input: "0000.0000.0000.0010", format: "%b", want: Data{0x02, 0x00}},
		{name: "binary one byte inference", input: "0000_0010", format: "%b", want: Data{0x02}},
		{name: "hex formatted", input: "0x0002", format: "%x", want: Data{0x02, 0x00}},
		{name: "hex lowercase", input: "00ff", format: "%x", want: Data{0xFF, 0x00}},
		{name: "hex uppercase", input: "00FF", format: "%x", want: Data{0xFF, 0x00}},
		{name: "hex separated", input: "0x00.02", format: "%x", want: Data{0x02, 0x00}},
		{name: "hex one byte inference", input: "0xFF", format: "%x", want: Data{0xFF}},
		{name: "unsigned fixed point", input: "1.000", format: "%u", want: Data{0xE8, 0x03}},
		{name: "unsigned integer", input: "1000", format: "%u", want: Data{0xE8, 0x03}},
		{name: "signed negative one", input: "-1", format: "%d", want: Data{0xFF}},
		{name: "signed negative two bytes", input: "-1000", format: "%d", want: Data{0x18, 0xFC}},
		{name: "unsigned two bytes", input: "256", format: "%u", want: Data{0x00, 0x01}},
		{name: "float32 exact", input: "1.5", format: "%f", want: Data{0x00, 0x00, 0xC0, 0x3F}},
		{name: "float32 decimal", input: "0.1", format: "%f", want: Data{0xCD, 0xCC, 0xCC, 0x3D}},
		{name: "float64 decimal", input: "0.10000000149011612", format: "%f", want: Data{0x00, 0x00, 0x00, 0xA0, 0x99, 0x99, 0xB9, 0x3F}},
		{name: "idn product", input: "P-0-0001", format: "%i", want: Data{0x01, 0x80}},
		{name: "idn structure", input: "S-1-0002.3.2", format: "%i", want: Data{0x02, 0x10, 0x02, 0x03}},
		{name: "time", input: "1970-01-01T00:00:01.500000000Z", format: "%t", want: Data{0x00, 0x65, 0xCD, 0x1D, 0x01, 0x00, 0x00, 0x00}},
		{name: "float length restriction", input: "1.5", format: "%f", initial: make(Data, 2), want: Data{0, 0}, wantErr: true},
		{name: "sticky unsigned length", input: "5", format: "%u", initial: make(Data, 4), want: Data{5, 0, 0, 0}},
		{name: "idn length clamps to four", input: "S-0-0001", format: "%i", initial: make(Data, 8), want: Data{0x01, 0, 0, 0}},
		{name: "sticky one-byte overflow", input: "256", format: "%u", initial: make(Data, 1), want: Data{0}, wantErr: true},
		{name: "noncanonical existing length infers", input: "5", format: "%u", initial: make(Data, 3), want: Data{5}},
		{name: "error preserves data", input: "abc", format: "%u", initial: Data{9}, want: Data{9}, wantErr: true},
		{name: "unsupported verb", input: "anything", format: "%v", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := append(Data(nil), test.initial...)
			_, err := fmt.Sscanf(test.input, test.format, &got)
			if test.wantErr {
				if err == nil {
					t.Fatalf("Sscanf(%q, %q) succeeded, want error", test.input, test.format)
				}
				if !reflect.DeepEqual(got, test.want) {
					t.Errorf("data after error = %v, want unchanged %v", []byte(got), []byte(test.want))
				}
				return
			}
			if err != nil {
				t.Fatalf("Sscanf(%q, %q) error = %v", test.input, test.format, err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("Sscanf(%q, %q) = %v, want %v", test.input, test.format, []byte(got), []byte(test.want))
			}
		})
	}

	t.Run("text tokens", func(t *testing.T) {
		var first, second Data
		n, err := fmt.Sscanf("abc def", "%s %s", &first, &second)
		if err != nil {
			t.Fatalf("Sscanf(text) error = %v", err)
		}
		if n != 2 || !reflect.DeepEqual(first, Data("abc")) || !reflect.DeepEqual(second, Data("def")) {
			t.Errorf("Sscanf(text) = %d, %q, %q; want 2, abc, def", n, []byte(first), []byte(second))
		}
	})

	t.Run("format terminators", func(t *testing.T) {
		var first, second Data
		n, err := fmt.Sscanf("1.5,2", "%f,%f", &first, &second)
		if err != nil {
			t.Fatalf("Sscanf(terminator) error = %v", err)
		}
		if n != 2 || !reflect.DeepEqual(first, Data{0x00, 0x00, 0xC0, 0x3F}) || !reflect.DeepEqual(second, Data{0x00, 0x00, 0x00, 0x40}) {
			t.Errorf("Sscanf(terminator) = %d, %v, %v", n, []byte(first), []byte(second))
		}
	})
}
