package sercos

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Data holds little-endian bytes that can be formatted as Sercos values.
type Data []byte

// String formats the data according to the specified attribute.
func (d Data) String(attr Attribute) string {
	switch attr.Format() {
	case Bin:
		return d.Bin(attr.Length())
	case Unsigned:
		return d.Unsigned(attr.Length(), attr.DecimalPlaces(), attr.ConversionFactor())
	case Signed:
		return d.Signed(attr.Length(), attr.DecimalPlaces(), attr.ConversionFactor())
	case Hex:
		return d.Hex(attr.Length())
	case Text:
		return d.Text()
	case IDN:
		return d.Idn(attr.Length())
	case Float:
		return d.Float(attr.Length())
	case Time:
		return d.Time()
	default:
		return fmt.Sprintf("invalid format %d", attr.Format())
	}
}

// Strings formats the data as a slice of strings according to the specified attribute.
func (d Data) Strings(attr Attribute) []string {
	switch attr.Format() {
	case Bin:
		return d.Bins(attr.Length())
	case Unsigned:
		return d.Unsigneds(attr.Length(), attr.DecimalPlaces(), attr.ConversionFactor())
	case Signed:
		return d.Signeds(attr.Length(), attr.DecimalPlaces(), attr.ConversionFactor())
	case Hex:
		return d.Hexen(attr.Length())
	case Text:
		return []string{d.Text()}
	case IDN:
		return d.Idns(attr.Length())
	case Float:
		return d.Floats(attr.Length())
	case Time:
		return d.Times()
	default:
		return []string{fmt.Sprintf("invalid format %d", attr.Format())}
	}
}

// Format implements fmt.Formatter. Width selects the data length in bytes (default 8),
// except for %s, which ignores it. Precision selects decimal places for %u and %d
// (default 0). Examples: %2b is 2-byte binary; %2.3u and %2.3d are 2-byte unsigned
// and signed values with 3 decimal places; %2x or %2X is 2-byte hex (always uppercase); %s is text; %2i is IDN
// (width clamped to 2-4); %4f is a 4-byte float; %t is 8 byte time, width ignored. Widths below 4 for %f
// produce an invalid-length message. Unsupported verbs produce no output.
func (d Data) Format(s fmt.State, verb rune) { // nolint:cyclop
	length, ok := s.Width()
	if !ok {
		length = 8
	}
	decimalPlaces, ok := s.Precision()
	if !ok {
		decimalPlaces = 0
	}
	switch verb {
	case 'b':
		fmt.Fprint(s, d.Bin(length))
	case 'u':
		fmt.Fprint(s, d.Unsigned(length, decimalPlaces, 1))
	case 'd':
		fmt.Fprint(s, d.Signed(length, decimalPlaces, 1))
	case 'x', 'X':
		fmt.Fprint(s, d.Hex(length))
	case 's':
		fmt.Fprint(s, d.Text())
	case 'i':
		if length > 4 {
			length = 4
		}
		if length < 2 {
			length = 2
		}
		fmt.Fprint(s, d.Idn(length))
	case 'f':
		if length < 4 {
			fmt.Fprintf(s, "invalid float length %d", length)
		}
		fmt.Fprint(s, d.Float(length))
	case 't':
		fmt.Fprint(s, d.Time())
	}
}

// Bin formats the data as a binary string with the specified length. Valid lengths are 1, 2, 4, and 8.
func (d Data) Bin(length int) string {
	switch length {
	case 1, 2, 4, 8:
	default:
		return fmt.Sprintf("invalid bin length %d", length)
	}

	var result strings.Builder
	result.Grow(2 + length*10)
	result.WriteString("0b")
	first := true
	for i := length - 1; i >= 0; i-- {
		var b byte
		if i < len(d) {
			b = d[i]
		}
		for shift := 4; shift >= 0; shift -= 4 {
			if !first {
				result.WriteByte('.')
			}
			fmt.Fprintf(&result, "%04b", (b>>shift)&0x0F)
			first = false
		}
	}
	return result.String()
}

// Bins formats the data as a slice of binary strings, each with the specified length. Valid lengths are 1, 2, 4, and 8. Returns nil if the data length is not divisible by the specified length.
func (d Data) Bins(length int) []string {
	switch length {
	case 1, 2, 4, 8:
	default:
		return []string{fmt.Sprintf("invalid bin length %d", length)}
	}
	if len(d)%length != 0 {
		return nil
	}

	bins := make([]string, 0, len(d)/length)
	for i := 0; i < len(d); i += length {
		bins = append(bins, d[i:i+length].Bin(length))
	}
	return bins
}

// Unsigned formats the first length bytes as an unsigned fixed-point value.
func (d Data) Unsigned(length, decimalPlaces, factor int) string {
	switch length {
	case 1, 2, 4, 8:
	default:
		return fmt.Sprintf("invalid unsigned length %d", length)
	}
	var buf [8]byte
	copy(buf[:length], d)

	value := new(big.Int).SetUint64(binary.LittleEndian.Uint64(buf[:]))
	value.Mul(value, big.NewInt(int64(factor)))
	return formatFixedPoint(value.String(), decimalPlaces)
}

// Unsigneds formats each length-byte chunk as an unsigned fixed-point value.
func (d Data) Unsigneds(length, decimalPlaces, factor int) []string {
	switch length {
	case 1, 2, 4, 8:
	default:
		return []string{fmt.Sprintf("invalid unsigned length %d", length)}
	}
	if len(d)%length != 0 {
		return nil
	}

	values := make([]string, 0, len(d)/length)
	for i := 0; i < len(d); i += length {
		values = append(values, d[i:i+length].Unsigned(length, decimalPlaces, factor))
	}
	return values
}

// Signed formats the first length bytes as a signed fixed-point value.
func (d Data) Signed(length, decimalPlaces, factor int) string {
	switch length {
	case 1, 2, 4, 8:
	default:
		return fmt.Sprintf("invalid signed length %d", length)
	}
	var buf [8]byte
	copy(buf[:length], d)

	value := int64(binary.LittleEndian.Uint64(buf[:])) //nolint:gosec
	bits := uint(length * 8)
	if bits < 64 {
		value = value << (64 - bits) >> (64 - bits)
	}
	value *= int64(factor)
	return formatFixedPoint(strconv.FormatInt(value, 10), decimalPlaces)
}

// Signeds formats each length-byte chunk as a signed fixed-point value.
func (d Data) Signeds(length, decimalPlaces, factor int) []string {
	switch length {
	case 1, 2, 4, 8:
	default:
		return []string{fmt.Sprintf("invalid signed length %d", length)}
	}
	if len(d)%length != 0 {
		return nil
	}

	values := make([]string, 0, len(d)/length)
	for i := 0; i < len(d); i += length {
		values = append(values, d[i:i+length].Signed(length, decimalPlaces, factor))
	}
	return values
}

func formatFixedPoint(formatted string, decimalPlaces int) string {
	if decimalPlaces <= 0 {
		return formatted
	}

	sign := ""
	digits := formatted
	if strings.HasPrefix(digits, "-") {
		sign = "-"
		digits = digits[1:]
	}
	if len(digits) <= decimalPlaces {
		return sign + "0." + digits + strings.Repeat("0", decimalPlaces-len(digits))
	}
	separator := len(digits) - decimalPlaces
	return sign + digits[:separator] + "." + digits[separator:]
}

// Hex formats the data as an uppercase hexadecimal string with the specified byte length.
func (d Data) Hex(length int) string {
	switch length {
	case 1, 2, 4, 8:
	default:
		return fmt.Sprintf("invalid hex length %d", length)
	}

	var result strings.Builder
	result.Grow(2 + length*2)
	result.WriteString("0x")
	for i := length - 1; i >= 0; i-- {
		var b byte
		if i < len(d) {
			b = d[i]
		}
		fmt.Fprintf(&result, "%02X", b)
	}
	return result.String()
}

// Hexen formats the data as a slice of hex strings, each with the specified length. Valid lengths are 1, 2, 4, and 8.
func (d Data) Hexen(length int) []string {
	switch length {
	case 1, 2, 4, 8:
	default:
		return []string{fmt.Sprintf("invalid hex length %d", length)}
	}
	if len(d)%length != 0 {
		return nil
	}

	hexen := make([]string, 0, len(d)/length)
	for i := 0; i < len(d); i += length {
		hexen = append(hexen, d[i:i+length].Hex(length))
	}
	return hexen
}

// Text returns the data as a string
func (d Data) Text() string {
	return string(d)
}

// Idn formats the data as an IDN. Length must be 2 for IdnS2 or 4 for Idn.
func (d Data) Idn(length int) string {
	switch length {
	case 2, 4:
	default:
		return fmt.Sprintf("invalid idn length %d", length)
	}

	var buf [4]byte
	copy(buf[:length], d)
	if length == 2 {
		return IdnS2(binary.LittleEndian.Uint16(buf[:2])).String()
	}
	return Idn(binary.LittleEndian.Uint32(buf[:])).String()
}

// Idns formats data as IDNs of length 2 or 4 and returns nil when the data length is not divisible by length.
func (d Data) Idns(length int) []string {
	switch length {
	case 2, 4:
	default:
		return []string{fmt.Sprintf("invalid idn length %d", length)}
	}
	if len(d)%length != 0 {
		return nil
	}

	idns := make([]string, 0, len(d)/length)
	for i := 0; i < len(d); i += length {
		idns = append(idns, d[i:i+length].Idn(length))
	}
	return idns
}

// Float formats the data as a floating-point number. Length must be 4 for float32 or 8 for float64.
func (d Data) Float(length int) string {
	var buf [8]byte
	switch length {
	case 4:
		copy(buf[:4], d)
		return fmt.Sprintf("%G", math.Float32frombits(binary.LittleEndian.Uint32(buf[:4])))
	case 8:
		copy(buf[:], d)
		return fmt.Sprintf("%G", math.Float64frombits(binary.LittleEndian.Uint64(buf[:])))
	default:
		return fmt.Sprintf("invalid float length %d", length)
	}
}

// Floats formats data as 4- or 8-byte floats and returns nil when the data length is not divisible by length.
func (d Data) Floats(length int) []string {
	switch length {
	case 4, 8:
	default:
		return []string{fmt.Sprintf("invalid float length %d", length)}
	}
	if len(d)%length != 0 {
		return nil
	}

	floats := make([]string, 0, len(d)/length)
	for i := 0; i < len(d); i += length {
		floats = append(floats, d[i:i+length].Float(length))
	}
	return floats
}

// Time formats the data as an time relative to the Unix epoch.
func (d Data) Time() string {
	var buf [8]byte
	copy(buf[:], d)
	nanos := int64(int32(binary.LittleEndian.Uint32(buf[:4]))) // nolint:gosec
	seconds := int64(binary.LittleEndian.Uint32(buf[4:]))
	polarity := int64(1)
	switch {
	case nanos >= 1e9 || nanos <= -1e9:
		nanos, polarity = 0, -1
	case nanos < 0:
		polarity = -1
	}
	dt := epochStart.Add(time.Duration(polarity * (seconds*1e9 + nanos)))
	return dt.Format(timeLayout)
}

const timeLayout = "2006-01-02T15:04:05.000000000Z" // dt.Format works only with exactly this time. See https://pkg.go.dev/time#pkg-constants
var epochStart = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

// Times formats data as times and returns nil when the data length is not divisible by 8.
func (d Data) Times() []string {
	if len(d)%8 != 0 {
		return nil
	}

	times := make([]string, 0, len(d)/8)
	for i := 0; i < len(d); i += 8 {
		times = append(times, d[i:i+8].Time())
	}
	return times
}
