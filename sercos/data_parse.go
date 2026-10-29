package sercos

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joomcode/errorx"
)

var (
	unsignedFixedPattern = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]+))?$`)
	signedFixedPattern   = regexp.MustCompile(`^([+-]?)([0-9]+)(?:\.([0-9]+))?$`)
	idnPattern           = regexp.MustCompile(`^(?:([SsPp])-)?(?:([0-9])-)?([0-9]+)(?:\.([0-9]+)\.([0-9]+))?$`)
	digitSeparators      = strings.NewReplacer(".", "", "_", "", " ", "")
)

func parseError(format string, args ...any) error {
	return errorx.EnsureStackTrace(fmt.Errorf(format, args...))
}

func digitsOf(s string, prefix byte) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '0' && (s[1] == prefix || s[1] == prefix-'a'+'A') {
		s = s[2:]
	}
	return digitSeparators.Replace(s)
}

func parseBin(s string, length int) (Data, error) {
	digits := digitsOf(s, 'b')
	if len(digits) == 0 || len(digits) > 8*length {
		return nil, parseError("sercos: invalid binary value %q", s)
	}
	for _, digit := range digits {
		if digit != '0' && digit != '1' {
			return nil, parseError("sercos: invalid binary value %q", s)
		}
	}
	value, ok := new(big.Int).SetString(digits, 2)
	if !ok {
		return nil, parseError("sercos: invalid binary value %q", s)
	}
	return encodeInt(value, length, false)
}

func parseHex(s string, length int) (Data, error) {
	digits := digitsOf(s, 'x')
	if len(digits) == 0 || len(digits) > 2*length {
		return nil, parseError("sercos: invalid hexadecimal value %q", s)
	}
	for _, digit := range digits {
		if !isHexDigit(digit) {
			return nil, parseError("sercos: invalid hexadecimal value %q", s)
		}
	}
	value, ok := new(big.Int).SetString(digits, 16)
	if !ok {
		return nil, parseError("sercos: invalid hexadecimal value %q", s)
	}
	return encodeInt(value, length, false)
}

func isHexDigit(digit rune) bool {
	return digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f' || digit >= 'A' && digit <= 'F'
}

func parseFixed(s string, length, decimalPlaces, factor int, signed bool) (Data, error) {
	raw, err := fixedValue(s, decimalPlaces, factor, signed)
	if err != nil {
		return nil, err
	}
	return encodeInt(raw, length, signed)
}

func fixedValue(s string, decimalPlaces, factor int, signed bool) (*big.Int, error) {
	s = strings.TrimSpace(s)
	pattern := unsignedFixedPattern
	if signed {
		pattern = signedFixedPattern
	}
	parts := pattern.FindStringSubmatch(s)
	if parts == nil {
		return nil, parseError("sercos: invalid fixed-point value %q", s)
	}
	var integer, fraction, sign string
	if signed {
		sign, integer, fraction = parts[1], parts[2], parts[3]
	} else {
		integer, fraction = parts[1], parts[2]
	}
	if len(fraction) > decimalPlaces {
		return nil, parseError("sercos: too many decimal places in %q", s)
	}
	if factor == 0 {
		return nil, parseError("sercos: conversion factor 0 for %q", s)
	}
	scaledText := integer + fraction + strings.Repeat("0", decimalPlaces-len(fraction))
	scaled, ok := new(big.Int).SetString(scaledText, 10)
	if !ok {
		return nil, parseError("sercos: invalid fixed-point value %q", s)
	}
	if sign == "-" {
		scaled.Neg(scaled)
	}
	factorValue := big.NewInt(int64(factor))
	raw, remainder := new(big.Int), new(big.Int)
	raw.QuoRem(scaled, factorValue, remainder)
	if remainder.Sign() != 0 {
		return nil, parseError("sercos: %q is not a multiple of conversion factor", s)
	}
	return raw, nil
}

func encodeInt(raw *big.Int, length int, signed bool) (Data, error) {
	bits := 8 * length
	if signed {
		limit := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
		minimum := new(big.Int).Neg(new(big.Int).Set(limit))
		if raw.Cmp(minimum) < 0 || raw.Cmp(limit) >= 0 {
			return nil, parseError("sercos: value out of range for %d bytes", length)
		}
	} else if raw.Sign() < 0 || raw.BitLen() > bits {
		return nil, parseError("sercos: value out of range for %d bytes", length)
	}
	mask := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	mask.Sub(mask, big.NewInt(1))
	value := new(big.Int).And(raw, mask).Uint64()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], value)
	return Data(append([]byte(nil), buf[:length]...)), nil
}

func parseFloat(s string, length int) (Data, error) {
	s = strings.TrimSpace(s)
	if length != 4 && length != 8 {
		return nil, parseError("sercos: invalid float length %d", length)
	}
	var buf [8]byte
	if length == 4 {
		value, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return nil, parseError("sercos: invalid float value %q: %v", s, err)
		}
		binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(float32(value)))
	} else {
		value, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, parseError("sercos: invalid float value %q: %v", s, err)
		}
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(value))
	}
	return Data(append([]byte(nil), buf[:length]...)), nil
}

func parseIdn(s string, length int) (Data, error) {
	s = strings.TrimSpace(s)
	if length != 2 && length != 4 {
		return nil, parseError("sercos: invalid idn length %d", length)
	}
	parts := idnPattern.FindStringSubmatch(s)
	if parts == nil {
		return nil, parseError("sercos: invalid IDN %q", s)
	}
	if length == 2 && parts[4] != "" {
		return nil, parseError("sercos: idn with structure fields needs length 4: %q", s)
	}
	value, err := idnValue(parts, s)
	if err != nil {
		return nil, err
	}
	var buf [4]byte
	if length == 2 {
		binary.LittleEndian.PutUint16(buf[:2], uint16(value)) // nolint:gosec
	} else {
		binary.LittleEndian.PutUint32(buf[:], value)
	}
	return Data(append([]byte(nil), buf[:length]...)), nil
}

func idnValue(parts []string, input string) (uint32, error) {
	set := 0
	if parts[2] != "" {
		var err error
		set, err = parseIDNField(parts[2], "set", input, 7)
		if err != nil {
			return 0, err
		}
	}
	number, err := parseIDNField(parts[3], "number", input, 4095)
	if err != nil {
		return 0, err
	}
	value := uint32(set<<12 | number) // nolint:gosec
	if parts[1] == "P" || parts[1] == "p" {
		value |= 0x8000
	}
	if parts[4] != "" {
		first, err := parseIDNField(parts[4], "structure field 1", input, 255)
		if err != nil {
			return 0, err
		}
		second, err := parseIDNField(parts[5], "structure field 2", input, 255)
		if err != nil {
			return 0, err
		}
		value |= uint32(first)<<24 | uint32(second)<<16 // nolint:gosec
	}
	return value, nil
}

func parseIDNField(text, field, input string, maximum int) (int, error) {
	value, err := strconv.Atoi(text)
	if err != nil || value > maximum {
		return 0, parseError("sercos: invalid IDN %q: %s out of range", input, field)
	}
	return value, nil
}

func parseTime(s string) (Data, error) {
	s = strings.TrimSpace(s)
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return nil, parseError("sercos: invalid time value %q: %v", s, err)
	}
	sec := t.Unix()
	ns := int64(t.Nanosecond())
	var seconds, nanos int64
	if sec >= 0 {
		seconds, nanos = sec, ns
	} else {
		if sec < -(1 << 32) {
			return nil, parseError("sercos: time out of range %q", s)
		}
		seconds = -sec
		if ns == 0 {
			nanos = -1e9
		} else {
			nanos = -ns
		}
	}
	if seconds > math.MaxUint32 {
		return nil, parseError("sercos: time out of range %q", s)
	}
	var buf [8]byte
	binary.LittleEndian.PutUint32(buf[:4], uint32(int32(nanos))) // nolint:gosec
	binary.LittleEndian.PutUint32(buf[4:], uint32(seconds))      // nolint:gosec
	return Data(append([]byte(nil), buf[:]...)), nil
}

// ParseData parses s, one element as printed by Data.String(attr), into little-endian data.
func ParseData(attr Attribute, s string) (Data, error) {
	switch attr.Format() {
	case Bin:
		return parseBin(s, attr.Length())
	case Unsigned:
		return parseFixed(s, attr.Length(), attr.DecimalPlaces(), attr.ConversionFactor(), false)
	case Signed:
		return parseFixed(s, attr.Length(), attr.DecimalPlaces(), attr.ConversionFactor(), true)
	case Hex:
		return parseHex(s, attr.Length())
	case Text:
		return Data(s), nil
	case IDN:
		return parseIdn(s, attr.Length())
	case Float:
		return parseFloat(s, attr.Length())
	case Time:
		return parseTime(s)
	default:
		return nil, parseError("sercos: invalid format %d", attr.Format())
	}
}

// ParseDatas parses each string of ss, as returned by Data.Strings(attr), and concatenates the results.
func ParseDatas(attr Attribute, ss []string) (Data, error) {
	out := Data{}
	for i, s := range ss {
		d, err := ParseData(attr, s)
		if err != nil {
			return nil, errorx.EnsureStackTrace(fmt.Errorf("sercos: element %d: %w", i, err))
		}
		out = append(out, d...)
	}
	return out, nil
}
