package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	labelWidth   = 15
	defaultWidth = 80
	elemSep      = "  "
)

// terminalWidth returns the column count of stdout, or defaultWidth if stdout is not a terminal or reports a width <= 0.
func terminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return defaultWidth
	}
	return width
}

// writeRaw prints data as a hex dump.
func writeRaw(w io.Writer, width int, label string, data []byte) {
	if labelWidth+3*len(data)-1 <= width {
		fmt.Fprintf(w, "%-*s%s\n", labelWidth, label+":", formatBytes(data))
		return
	}

	fmt.Fprintf(w, "%s:\n", label)
	offsetWidth := hexOffsetWidth(len(data) - 1)
	prefix := offsetWidth + 2
	bytesPerRow := (width - prefix + 1) / 3
	if bytesPerRow >= 8 {
		bytesPerRow -= bytesPerRow % 8
	}
	if bytesPerRow < 1 {
		bytesPerRow = 1
	}

	multipleRows := len(data) > bytesPerRow
	for offset := 0; offset < len(data); offset += bytesPerRow {
		end := offset + bytesPerRow
		if end > len(data) {
			end = len(data)
		}
		if multipleRows {
			fmt.Fprintf(w, "%0*x  ", offsetWidth, offset)
		}
		fmt.Fprintf(w, "%s\n", formatBytes(data[offset:end]))
	}
}

// sanitizeText replaces what could act as a terminal command or spoof the layout in text that a device supplied:
// control characters (C0, 0x7f, C1, including newline and escape) and bytes that are not valid UTF-8 (a terminal
// in an 8-bit mode would read them as C1 controls). Each is replaced by '.', as the raw hex dumps show it.
// The raw bytes stay available in the hex dump.
func sanitizeText(s string) string {
	clean := true
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsControl(r) || r == utf8.RuneError && size == 1 {
			clean = false
			break
		}
		i += size
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsControl(r) || r == utf8.RuneError && size == 1 {
			b.WriteByte('.')
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// writeElements prints formatted element strings in the same grid layout. The elements are sanitized with
// sanitizeText because they can contain device text.
func writeElements(w io.Writer, width int, label string, elems []string) {
	var safe []string
	for i, elem := range elems {
		if clean := sanitizeText(elem); clean != elem {
			if safe == nil {
				safe = slices.Clone(elems)
			}
			safe[i] = clean
		}
	}
	if safe != nil {
		elems = safe
	}
	joined := strings.Join(elems, elemSep)
	if len(elems) == 0 || labelWidth+len(joined) <= width {
		fmt.Fprintf(w, "%-*s%s\n", labelWidth, label+":", joined)
		return
	}

	fmt.Fprintf(w, "%s:\n", label)
	cellWidth := 0
	for _, elem := range elems {
		if n := utf8.RuneCountInString(elem); n > cellWidth {
			cellWidth = n
		}
	}
	offsetWidth := decimalOffsetWidth(len(elems) - 1)
	prefix := offsetWidth + 2
	elementsPerRow := (width - prefix + len(elemSep)) / (cellWidth + len(elemSep))
	if elementsPerRow < 1 {
		elementsPerRow = 1
	}

	writeElementRows(w, elems, offsetWidth, cellWidth, elementsPerRow)
}

func writeElementRows(w io.Writer, elems []string, offsetWidth, cellWidth, elementsPerRow int) {
	multipleRows := len(elems) > elementsPerRow
	for offset := 0; offset < len(elems); offset += elementsPerRow {
		end := offset + elementsPerRow
		if end > len(elems) {
			end = len(elems)
		}
		if multipleRows {
			fmt.Fprintf(w, "%*d  ", offsetWidth, offset)
		}
		for i, elem := range elems[offset:end] {
			if i > 0 {
				fmt.Fprint(w, elemSep)
			}
			fmt.Fprint(w, strings.Repeat(" ", cellWidth-utf8.RuneCountInString(elem)), elem)
		}
		fmt.Fprintln(w)
	}
}

func formatBytes(data []byte) string {
	var b strings.Builder
	if len(data) > 0 {
		b.Grow(3*len(data) - 1)
	}
	for i, value := range data {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%02x", value)
	}
	return b.String()
}

func hexOffsetWidth(lastOffset int) int {
	width := 1
	for lastOffset >= 16 {
		width++
		lastOffset >>= 4
	}
	if width < 4 {
		return 4
	}
	return width
}

func decimalOffsetWidth(lastOffset int) int {
	width := 1
	for lastOffset >= 10 {
		width++
		lastOffset /= 10
	}
	return width
}
