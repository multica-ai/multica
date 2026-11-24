package buffer

import "io"

// Reader implements an io.Reader over a byte slice.
type Reader struct {
	buf []byte
	pos int
}

// NewReader returns a new Reader for a given byte slice.
func NewReader(buf []byte) *Reader {
	return &Reader{
		buf: buf,
	}
}

// Read implements io.Reader.
func (r *Reader) Read(b []byte) (int, error) {
	if len(r.buf) <= r.pos {
		return 0, io.EOF
	}
	n := copy(b, r.buf[r.pos:])
	r.pos += n
	return n, nil
}

// ReadAt implements io.ReaderAt.
func (r *Reader) ReadAt(b []byte, off int64) (int, error) {
	if int64(len(r.buf)) <= off {
		return 0, io.EOF
	}
	return copy(b, r.buf[off:]), nil
}

// Bytes returns the underlying byte slice.
func (r *Reader) Bytes() []byte {
	return r.buf
}

// Reset resets the position of the read pointer to the beginning of the underlying byte slice.
func (r *Reader) Reset() {
	r.pos = 0
}

// Len returns the length of the buffer.
func (r *Reader) Len() int {
	return len(r.buf)
}
