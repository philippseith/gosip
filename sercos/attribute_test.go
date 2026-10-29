package sercos

import "testing"

func TestNewAttributeOptions(t *testing.T) {
	attribute := NewAttribute(
		WithConversionFactor(0x1234),
		WithLength(4),
		WithFormat(Float),
		WithDecimalPlaces(5),
		WithList(true),
	)

	if got := attribute.ConversionFactor(); got != 0x1234 {
		t.Errorf("ConversionFactor() = %d, want %d", got, 0x1234)
	}
	if got := attribute.Length(); got != 4 {
		t.Errorf("Length() = %d, want 4", got)
	}
	if got := attribute.Format(); got != Float {
		t.Errorf("Format() = %d, want %d", got, Float)
	}
	if got := attribute.DecimalPlaces(); got != 5 {
		t.Errorf("DecimalPlaces() = %d, want 5", got)
	}
	if !attribute.IsList() {
		t.Error("IsList() = false, want true")
	}

	attribute = NewAttribute(WithList(true), WithList(false))
	if attribute.IsList() {
		t.Error("IsList() = true after WithList(false), want false")
	}
}

func TestWithLengthIgnoresUnsupportedLength(t *testing.T) {
	attribute := NewAttribute(WithLength(4), WithLength(3))
	if got := attribute.Length(); got != 4 {
		t.Errorf("Length() = %d after unsupported length, want 4", got)
	}
}
