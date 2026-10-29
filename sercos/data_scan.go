package sercos

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

var (
	_           fmt.Scanner = (*Data)(nil)
	scanLengths             = [...]int{1, 2, 4, 8}
)

// Scan implements fmt.Scanner. Verbs mirror Data.Format: b, u, d, x, X, s, i, f, t.
// The byte length is sticky when d already has length 1, 2, 4, or 8; use an empty Data to infer it.
func (d *Data) Scan(state fmt.ScanState, verb rune) error {
	token, err := scanToken(state, verb)
	if err != nil {
		return err
	}
	result, err := parseScanValue(string(token), verb, len(*d))
	if err != nil {
		return err
	}
	*d = result
	return nil
}

func scanToken(state fmt.ScanState, verb rune) ([]byte, error) {
	accept, supported := scanPredicate(verb)
	if !supported {
		return nil, parseError("sercos: unsupported scan verb %q", verb)
	}
	token, err := state.Token(true, accept)
	if len(token) == 0 {
		return nil, parseError("sercos: no %c value in input", verb)
	}
	return token, err
}

func scanPredicate(verb rune) (func(rune) bool, bool) {
	switch verb {
	case 'b':
		return isBinaryScanRune, true
	case 'x', 'X':
		return isHexScanRune, true
	case 'u':
		return isUnsignedScanRune, true
	case 'd':
		return isSignedScanRune, true
	case 'f':
		return isFloatScanRune, true
	case 'i':
		return isIDNScanRune, true
	case 't':
		return isTimeScanRune, true
	case 's':
		return nil, true
	default:
		return nil, false
	}
}

func isBinaryScanRune(r rune) bool {
	return r == '0' || r == '1' || r == '.' || r == '_' || r == 'b' || r == 'B'
}

func isHexScanRune(r rune) bool {
	return r == '.' || r == '_' || r == 'x' || r == 'X' || isHexDigit(r)
}

func isUnsignedScanRune(r rune) bool {
	return r >= '0' && r <= '9' || r == '.'
}

func isSignedScanRune(r rune) bool {
	return r >= '0' && r <= '9' || r == '.' || r == '+' || r == '-'
}

func isFloatScanRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '+' || r == '-'
}

func isIDNScanRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.'
}

func isTimeScanRune(r rune) bool {
	return r >= '0' && r <= '9' || r == '-' || r == ':' || r == '.' || r == 'T' || r == 'Z'
}

func parseScanValue(token string, verb rune, currentLength int) (Data, error) {
	if verb == 's' {
		return Data(append([]byte(nil), token...)), nil
	}
	if verb == 't' {
		return parseTime(token)
	}
	length, err := scanByteLength(token, verb, currentLength)
	if err != nil {
		return nil, err
	}
	switch verb {
	case 'b':
		return parseBin(token, length)
	case 'x', 'X':
		return parseHex(token, length)
	case 'u', 'd':
		return parseFixed(token, length, fractionDigits(token), 1, verb == 'd')
	case 'f':
		return parseFloat(token, length)
	case 'i':
		return parseIdn(token, length)
	default:
		return nil, parseError("sercos: unsupported scan verb %q", verb)
	}
}

func scanByteLength(token string, verb rune, currentLength int) (int, error) {
	if currentLength == 1 || currentLength == 2 || currentLength == 4 || currentLength == 8 {
		if verb == 'i' {
			if currentLength < 2 {
				return 2, nil
			}
			if currentLength > 4 {
				return 4, nil
			}
		}
		return currentLength, nil
	}
	return inferScanLength(token, verb)
}

func inferScanLength(token string, verb rune) (int, error) {
	switch verb {
	case 'b':
		return inferBinaryLength(token)
	case 'x', 'X':
		return inferHexLength(token)
	case 'u', 'd':
		return inferFixedLength(token, verb == 'd')
	case 'f':
		return inferFloatLength(token)
	case 'i':
		if strings.ContainsRune(token, '.') {
			return 4, nil
		}
		return 2, nil
	default:
		return 0, parseError("sercos: unsupported scan verb %q", verb)
	}
}

func inferBinaryLength(token string) (int, error) {
	n := len(digitsOf(token, 'b'))
	if length, ok := smallestScanLength(n, 8); ok {
		return length, nil
	}
	return 0, parseError("sercos: invalid binary value %q", token)
}

func inferHexLength(token string) (int, error) {
	n := len(digitsOf(token, 'x'))
	if length, ok := smallestScanLength(n, 2); ok {
		return length, nil
	}
	return 0, parseError("sercos: invalid hexadecimal value %q", token)
}

func smallestScanLength(n, digitsPerByte int) (int, bool) {
	for _, length := range scanLengths {
		if n > 0 && n <= digitsPerByte*length {
			return length, true
		}
	}
	return 0, false
}

func inferFixedLength(token string, signed bool) (int, error) {
	decimalPlaces := fractionDigits(token)
	raw, err := fixedValue(token, decimalPlaces, 1, signed)
	if err != nil {
		return 0, err
	}
	for _, length := range scanLengths {
		if fitsInteger(raw, length, signed) {
			return length, nil
		}
	}
	_, err = encodeInt(raw, 8, signed)
	return 0, err
}

func inferFloatLength(token string) (int, error) {
	value, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return 0, parseError("sercos: invalid float value %q: %v", token, err)
	}
	float32Text := strconv.FormatFloat(float64(float32(value)), 'g', -1, 32)
	float64Text := strconv.FormatFloat(value, 'g', -1, 64)
	if float32Text == float64Text {
		return 4, nil
	}
	return 8, nil
}

func fractionDigits(token string) int {
	if dot := strings.IndexByte(token, '.'); dot >= 0 {
		return len(token) - dot - 1
	}
	return 0
}

func fitsInteger(value *big.Int, length int, signed bool) bool {
	bitSize := 8 * length
	if !signed {
		return value.Sign() >= 0 && value.BitLen() <= bitSize
	}
	if value.Sign() >= 0 {
		return value.BitLen() <= bitSize-1
	}
	if value.BitLen() < bitSize {
		return true
	}
	return value.BitLen() == bitSize && value.TrailingZeroBits() == uint(bitSize-1)
}
