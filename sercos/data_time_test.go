package sercos

import (
	"reflect"
	"testing"
)

func TestDataTimeShort(t *testing.T) {
	const epoch = "1970-01-01T00:00:00.000000000Z"
	if got := (Data{}).Time(); got != epoch {
		t.Errorf("Time() for empty data = %q, want %q", got, epoch)
	}
	if got := (Data{0x00, 0x65, 0xCD, 0x1D}).Time(); got != "1970-01-01T00:00:00.500000000Z" {
		t.Errorf("Time() for 4-byte data = %q, want 500ms after epoch", got)
	}
}

func TestDataTimes(t *testing.T) {
	data := Data{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x65, 0xCD, 0x1D, 0x01, 0x00, 0x00, 0x00,
	}
	got := data.Times()
	want := []string{"1970-01-01T00:00:00.000000000Z", "1970-01-01T00:00:01.500000000Z"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Times() = %v, want %v", got, want)
	}

	if got := data[:9].Times(); got != nil {
		t.Errorf("Times() = %v, want nil for non-divisible data length", got)
	}
}
