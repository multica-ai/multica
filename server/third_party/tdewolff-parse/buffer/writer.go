package buffer

import (
	"io"
)

// Writer implements an io.Writer over a byte slice.
type Writer struct {
	buf    []byte
	err    error
	expand bool
}

// NewWriter returns a new Writer for a given byte slice.
func NewWriter(buf []byte) *Writer {
	return &Writer{
		buf:    buf,
		expand: true,
	}
}

// NewStaticWriter returns a new Writer for a given byte slice. It does not reallocate and expand the byte-slice.
func NewStaticWriter(buf []byte) *Writer {
	return &Writer{
		buf:    buf,
		expand: false,
	}
}

// Write writes bytes from the given byte slice and returns the number of bytes written and an error if occurred. When err != nil, n == 0.
func (w *Writer) Write(b []byte) (int, error) {
	n := len(b)
	end := len(w.buf)
	if end+n > cap(w.buf) {
		if !w.expand {
			w.err = io.EOF
			return 0, io.EOF
		}
		buf := make([]byte, end, 2*cap(w.buf)+n)
		copy(buf, w.buf)
		w.buf = buf
	}
	w.buf = w.buf[:end+n]
	return copy(w.buf[end:], b), nil
}

// Len returns the length of the underlying byte slice.
func (w *Writer) Len() int {
	return len(w.buf)
}

// Bytes returns the underlying byte slice.
func (w *Writer) Bytes() []byte {
	return w.buf
}

// Reset empties and reuses the current buffer. Subsequent writes will overwrite the buffer, so any reference to the underlying slice is invalidated after this call.
func (w *Writer) Reset() {
	w.buf = w.buf[:0]
}

// Close returns the last error.
func (w *Writer) Close() error {
	return w.err
}
