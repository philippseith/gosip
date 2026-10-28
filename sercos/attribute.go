package sercos

import "math/bits"

type Attribute uint32

type Format int

const (
	Bin Format = iota
	Unsigned
	Signed
	Hex
	Text
	IDN
	Float
	Time
)

func (a Attribute) ConversionFactor() int {
	return int(a & 0xFFFF)
}

func (a Attribute) Length() int {
	return 1 << ((uint32(a) >> 16) & 3)
}

func (a Attribute) Format() Format {
	return Format((uint32(a) & 0x00F00000) >> 20)
}

func (a Attribute) DecimalPlaces() int {
	return int((uint32(a) & 0x0F000000) >> 24)
}

func (a Attribute) IsList() bool {
	return (uint32(a) & 0x00040000) != 0
}

type AttributeOption func(*Attribute)

func NewAttribute(optione ...AttributeOption) Attribute {
	a := Attribute(1)
	for _, option := range optione {
		option(&a)
	}
	return a
}

func WithConversionFactor(factor int) AttributeOption {
	return func(a *Attribute) {
		*a = Attribute(uint32(*a)&^0x0000FFFF | uint32(factor)&0x0000FFFF) // nolint:gosec
	}
}

func WithLength(length int) AttributeOption {
	if length < 1 || length > 8 || length&(length-1) != 0 {
		return func(*Attribute) {}
	}
	lengthCode := uint32(bits.TrailingZeros(uint(length))) // nolint:gosec
	return func(a *Attribute) {
		*a = Attribute(uint32(*a)&^0x00030000 | lengthCode<<16)
	}
}

func WithFormat(format Format) AttributeOption {
	return func(a *Attribute) {
		*a = Attribute(uint32(*a)&^0x00F00000 | uint32(format)&0xF<<20) // nolint:gosec
	}
}

func WithDecimalPlaces(decimalPlaces int) AttributeOption {
	return func(a *Attribute) {
		*a = Attribute(uint32(*a)&^0x0F000000 | uint32(decimalPlaces)&0xF<<24) // nolint:gosec
	}
}

func WithList(isList bool) AttributeOption {
	return func(a *Attribute) {
		value := uint32(*a) &^ 0x00040000
		if isList {
			value |= 0x00040000
		}
		*a = Attribute(value)
	}
}
