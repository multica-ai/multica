package parse

import (
	"fmt"
	"io"
	"unicode"
)

// Copy returns a copy of the given byte slice.
func Copy(src []byte) (dst []byte) {
	dst = make([]byte, len(src))
	copy(dst, src)
	return
}

// ToLower converts all characters in the byte slice from A-Z to a-z.
func ToLower(src []byte) []byte {
	for i, c := range src {
		if c >= 'A' && c <= 'Z' {
			src[i] = c + ('a' - 'A')
		}
	}
	return src
}

// EqualFold returns true when s matches case-insensitively the targetLower (which must be lowercase).
func EqualFold(s, targetLower []byte) bool {
	if len(s) != len(targetLower) {
		return false
	}
	for i, c := range targetLower {
		d := s[i]
		if d != c && (d < 'A' || d > 'Z' || d+('a'-'A') != c) {
			return false
		}
	}
	return true
}

// Printable returns a printable string for given rune
func Printable(r rune) string {
	if unicode.IsGraphic(r) {
		return fmt.Sprintf("%c", r)
	} else if r < 128 {
		return fmt.Sprintf("0x%02X", r)
	}
	return fmt.Sprintf("%U", r)
}

var whitespaceTable = [256]bool{
	// ASCII
	false, false, false, false, false, false, false, false,
	false, true, true, false, true, true, false, false, // tab, new line, form feed, carriage return
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	true, false, false, false, false, false, false, false, // space
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	// non-ASCII
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
}

// IsWhitespace returns true for space, \n, \r, \t, \f.
func IsWhitespace(c byte) bool {
	return whitespaceTable[c]
}

var newlineTable = [256]bool{
	// ASCII
	false, false, false, false, false, false, false, false,
	false, false, true, false, false, true, false, false, // new line, carriage return
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	// non-ASCII
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
}

// IsNewline returns true for \n, \r.
func IsNewline(c byte) bool {
	return newlineTable[c]
}

// IsAllWhitespace returns true when the entire byte slice consists of space, \n, \r, \t, \f.
func IsAllWhitespace(b []byte) bool {
	for _, c := range b {
		if !IsWhitespace(c) {
			return false
		}
	}
	return true
}

// TrimWhitespace removes any leading and trailing whitespace characters.
func TrimWhitespace(b []byte) []byte {
	n := len(b)
	start := n
	for i := 0; i < n; i++ {
		if !IsWhitespace(b[i]) {
			start = i
			break
		}
	}
	end := n
	for i := n - 1; i >= start; i-- {
		if !IsWhitespace(b[i]) {
			end = i + 1
			break
		}
	}
	return b[start:end]
}

type Indenter struct {
	io.Writer
	b []byte
}

func NewIndenter(w io.Writer, n int) Indenter {
	if wi, ok := w.(Indenter); ok {
		w = wi.Writer
		n += len(wi.b)
	}

	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return Indenter{
		Writer: w,
		b:      b,
	}
}

func (in Indenter) Indent() int {
	return len(in.b)
}

func (in Indenter) Write(b []byte) (int, error) {
	n, j := 0, 0
	for i, c := range b {
		if c == '\n' {
			m, _ := in.Writer.Write(b[j : i+1])
			n += m
			m, _ = in.Writer.Write(in.b)
			n += m
			j = i + 1
		}
	}
	m, err := in.Writer.Write(b[j:])
	return n + m, err
}
