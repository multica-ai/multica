package parse

import (
	"bytes"
	"fmt"
	"io"
)

// Error is a parsing error returned by parser. It contains a message and an offset at which the error occurred.
type Error struct {
	Message string
	Line    int
	Column  int
	Context string
}

// NewError creates a new error
func NewError(r io.Reader, offset int, message string, a ...interface{}) *Error {
	line, column, context := Position(r, offset)
	if 0 < len(a) {
		message = fmt.Sprintf(message, a...)
	}
	return &Error{
		Message: message,
		Line:    line,
		Column:  column,
		Context: context,
	}
}

// NewErrorLexer creates a new error from an active Lexer.
func NewErrorLexer(l *Input, message string, a ...interface{}) *Error {
	r := bytes.NewBuffer(l.Bytes())
	offset := l.Offset()
	return NewError(r, offset, message, a...)
}

// Position returns the line, column, and context of the error.
// Context is the entire line at which the error occurred.
func (e *Error) Position() (int, int, string) {
	return e.Line, e.Column, e.Context
}

// Error returns the error string, containing the context and line + column number.
func (e *Error) Error() string {
	return fmt.Sprintf("%s on line %d and column %d\n%s", e.Message, e.Line, e.Column, e.Context)
}
