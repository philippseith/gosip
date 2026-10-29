package sercos

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseData(t *testing.T) {
	tests := []struct {
		name    string
		attr    Attribute
		input   string
		want    Data
		wantErr bool
	}{
		{name: "binary formatted", attr: 0x00010001, input: "0b0000.0000.0000.0010", want: Data{0x02, 0x00}},
		{name: "binary uppercase prefix", attr: 0x00010001, input: "0B0000.0000.0000.0010", want: Data{0x02, 0x00}},
		{name: "binary without prefix", attr: 0x00010001, input: "0000.0000.0000.0010", want: Data{0x02, 0x00}},
		{name: "binary without separators", attr: 0x00010001, input: "0000000000000010", want: Data{0x02, 0x00}},
		{name: "binary underscore separators", attr: 0x00010001, input: "0b0000_0000_0000_0010", want: Data{0x02, 0x00}},
		{name: "binary arbitrary separators", attr: 0x00010001, input: "0b00.00.0000.00.0010", want: Data{0x02, 0x00}},
		{name: "binary short value", attr: 0x00010001, input: "0b10", want: Data{0x02, 0x00}},
		{name: "binary too wide", attr: 0x00010001, input: "0b0000.0000.0000.0010.0", wantErr: true},
		{name: "binary invalid digit", attr: 0x00010001, input: "0b102", wantErr: true},
		{name: "unsigned fixed point", attr: 0x03110001, input: "1.000", want: Data{0xE8, 0x03}},
		{name: "unsigned excess decimals", attr: 0x03110001, input: "1.0000", wantErr: true},
		{name: "unsigned negative", attr: 0x03110001, input: "-1.000", wantErr: true},
		{name: "unsigned out of range", attr: 0x03110001, input: "65.536", wantErr: true},
		{name: "unsigned integer", attr: 0x00110001, input: "1000", want: Data{0xE8, 0x03}},
		{name: "unsigned conversion factor", attr: 0x00110005, input: "10", want: Data{0x02, 0x00}},
		{name: "unsigned conversion factor remainder", attr: 0x00110005, input: "11", wantErr: true},
		{name: "signed negative", attr: 0x00210001, input: "-1000", want: Data{0x18, 0xFC}},
		{name: "signed positive overflow", attr: 0x00210001, input: "32768", wantErr: true},
		{name: "signed minimum", attr: 0x00210001, input: "-32768", want: Data{0x00, 0x80}},
		{name: "hex formatted", attr: 0x00310001, input: "0x0102", want: Data{0x02, 0x01}},
		{name: "hex uppercase prefix", attr: 0x00310001, input: "0X0102", want: Data{0x02, 0x01}},
		{name: "hex without prefix", attr: 0x00310001, input: "0102", want: Data{0x02, 0x01}},
		{name: "hex dot separators", attr: 0x00310001, input: "01.02", want: Data{0x02, 0x01}},
		{name: "hex underscore separators", attr: 0x00310001, input: "0x01_02", want: Data{0x02, 0x01}},
		{name: "hex lowercase", attr: 0x00310001, input: "ff", want: Data{0xFF, 0x00}},
		{name: "hex uppercase", attr: 0x00310001, input: "FF", want: Data{0xFF, 0x00}},
		{name: "hex mixed case", attr: 0x00310001, input: "0xaBcD", want: Data{0xCD, 0xAB}},
		{name: "hex too wide", attr: 0x00310001, input: "0x10000", wantErr: true},
		{name: "hex invalid", attr: 0x00310001, input: "0xZZ", wantErr: true},
		{name: "text preserves spaces", attr: 0x00410001, input: " ab c ", want: Data(" ab c ")},
		{name: "idn product", attr: 0x00510001, input: "P-0-0001", want: Data{0x01, 0x80}},
		{name: "idn structure requires four bytes", attr: 0x00510001, input: "S-0-0001.0.0", wantErr: true},
		{name: "idn structure", attr: 0x00520001, input: "S-1-0002.3.2", want: Data{0x02, 0x10, 0x02, 0x03}},
		{name: "idn set out of range", attr: 0x00520001, input: "S-8-0001", wantErr: true},
		{name: "float32", attr: 0x00620001, input: "1.5", want: Data{0x00, 0x00, 0xC0, 0x3F}},
		{name: "float overflow", attr: 0x00620001, input: "1E+40", wantErr: true},
		{name: "float invalid length", attr: 0x00610001, input: "1.5", wantErr: true},
		{name: "time after epoch", attr: 0x00730001, input: "1970-01-01T00:00:01.500000000Z", want: Data{0x00, 0x65, 0xCD, 0x1D, 0x01, 0x00, 0x00, 0x00}},
		{name: "time before epoch", attr: 0x00730001, input: "1969-12-31T23:59:58.000000000Z", want: Data{0x00, 0x36, 0x65, 0xC4, 0x02, 0x00, 0x00, 0x00}},
		{name: "time wrong layout", attr: 0x00730001, input: "1970-01-01", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseData(test.attr, test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseData(%#x, %q) succeeded, want error", test.attr, test.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseData(%#x, %q) error = %v", test.attr, test.input, err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("ParseData(%#x, %q) = %v, want %v", test.attr, test.input, []byte(got), []byte(test.want))
			}
		})
	}

	roundTrips := []struct {
		name string
		attr Attribute
		data Data
	}{
		{name: "binary", attr: 0x00010001, data: Data{0x02, 0x00}},
		{name: "unsigned", attr: 0x03110001, data: Data{0xE8, 0x03}},
		{name: "signed", attr: 0x00210001, data: Data{0x18, 0xFC}},
		{name: "hex", attr: 0x00310001, data: Data{0x02, 0x01}},
		{name: "text", attr: 0x00410001, data: Data("some text")},
		{name: "idn two byte", attr: 0x00510001, data: Data{0x01, 0x80}},
		{name: "idn four byte", attr: 0x00520001, data: Data{0x02, 0x10, 0x02, 0x03}},
		{name: "float", attr: 0x00620001, data: Data{0x00, 0x00, 0xC0, 0x3F}},
	}
	for _, test := range roundTrips {
		t.Run("round trip "+test.name, func(t *testing.T) {
			got, err := ParseDatas(test.attr, test.data.Strings(test.attr))
			if err != nil {
				t.Fatalf("ParseDatas() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.data) {
				t.Errorf("ParseDatas() = %v, want %v", []byte(got), []byte(test.data))
			}
		})
	}

	listAttr := Attribute(0x00110001)
	listData := Data{0xE8, 0x03, 0x01, 0x00}
	got, err := ParseDatas(listAttr, listData.Strings(listAttr))
	if err != nil {
		t.Fatalf("ParseDatas(list) error = %v", err)
	}
	if !reflect.DeepEqual(got, listData) {
		t.Errorf("ParseDatas(list) = %v, want %v", []byte(got), []byte(listData))
	}

	preEpoch := "1969-12-31T23:59:58.000000000Z"
	timeAttr := Attribute(0x00730001)
	parsedTime, err := ParseDatas(timeAttr, []string{preEpoch})
	if err != nil {
		t.Fatalf("ParseDatas(pre-epoch time) error = %v", err)
	}
	if got := parsedTime.Time(); got != preEpoch {
		t.Errorf("pre-epoch round trip = %q, want %q", got, preEpoch)
	}
}

func TestParseDatas(t *testing.T) {
	_, err := ParseDatas(0x00110001, []string{"1", "x"})
	if err == nil || !strings.Contains(err.Error(), "element 1") {
		t.Errorf("ParseDatas error = %v, want element index", err)
	}
	for _, ss := range [][]string{nil, {}} {
		got, err := ParseDatas(0x00110001, ss)
		if err != nil {
			t.Fatalf("ParseDatas(empty) error = %v", err)
		}
		if !reflect.DeepEqual(got, Data{}) {
			t.Errorf("ParseDatas(empty) = %#v, want non-nil empty Data", []byte(got))
		}
	}
}
