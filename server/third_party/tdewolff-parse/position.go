package parse

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Position returns the line and column number for a certain position in a file. It is useful for recovering the position in a file that caused an error.
// It only treates \n, \r, and \r\n as newlines, which might be different from some languages also recognizing \f, \u2028, and \u2029 to be newlines.
func Position(r io.Reader, offset int) (line, col int, context string) {
	l := NewInput(r)
	line = 1
	for l.Pos() < offset {
		c := l.Peek(0)
		n := 1
		newline := false
		if c == '\n' {
			newline = true
		} else if c == '\r' {
			if l.Peek(1) == '\n' {
				newline = true
				n = 2
			} else {
				newline = true
			}
		} else if c >= 0xC0 {
			var r rune
			if r, n = l.PeekRune(0); r == '\u2028' || r == '\u2029' {
				newline = true
			}
		} else if c == 0 && l.Err() != nil {
			break
		}

		if 1 < n && offset < l.Pos()+n {
			break
		}
		l.Move(n)

		if newline {
			line++
			offset -= l.Pos()
			l.Skip()
		}
	}

	col = len([]rune(string(l.Lexeme()))) + 1
	context = positionContext(l, line, col)
	return
}

func positionContext(l *Input, line, col int) (context string) {
	for {
		c := l.Peek(0)
		if c == 0 && l.Err() != nil || c == '\n' || c == '\r' {
			break
		}
		l.Move(1)
	}
	rs := []rune(string(l.Lexeme()))

	// cut off front or rear of context to stay between 60 characters
	limit := 60
	offset := 20
	ellipsisFront := ""
	ellipsisRear := ""
	if limit < len(rs) {
		if col <= limit-offset {
			ellipsisRear = "..."
			rs = rs[:limit-3]
		} else if col >= len(rs)-offset-3 {
			ellipsisFront = "..."
			col -= len(rs) - offset - offset - 7
			rs = rs[len(rs)-offset-offset-4:]
		} else {
			ellipsisFront = "..."
			ellipsisRear = "..."
			rs = rs[col-offset-1 : col+offset]
			col = offset + 4
		}
	}

	// replace unprintable characters by a space
	for i, r := range rs {
		if !unicode.IsGraphic(r) {
			rs[i] = '·'
		}
	}

	context += fmt.Sprintf("%5d: %s%s%s\n", line, ellipsisFront, string(rs), ellipsisRear)
	context += fmt.Sprintf("%s^", strings.Repeat(" ", 6+col))
	return
}
