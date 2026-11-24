package buffer

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/tdewolff/test"
)

func TestReader(t *testing.T) {
	s := []byte("abcde")
	r := NewReader(s)
	test.T(t, r.Len(), 5, "len")
	test.Bytes(t, r.Bytes(), s, "reader must return bytes stored")

	buf := make([]byte, 3)
	n, err := r.Read(buf)
	test.T(t, err, nil, "error")
	test.That(t, n == 3, "first read must read 3 characters")
	test.Bytes(t, buf, []byte("abc"), "first read must match 'abc'")

	n, err = r.Read(buf)
	test.T(t, err, nil, "error")
	test.That(t, n == 2, "second read must read 2 characters")
	test.Bytes(t, buf[:n], []byte("de"), "second read must match 'de'")

	n, err = r.Read(buf)
	test.T(t, err, io.EOF, "error")
	test.That(t, n == 0, "third read must read 0 characters")

	n, err = r.Read(nil)
	test.T(t, err, io.EOF, "error")
	test.That(t, n == 0, "read to nil buffer must return 0 characters read")

	r.Reset()
	n, err = r.Read(buf)
	test.T(t, err, nil, "error")
	test.That(t, n == 3, "read after reset must read 3 characters")
	test.Bytes(t, buf, []byte("abc"), "read after reset must match 'abc'")
}

func ExampleNewReader() {
	r := NewReader([]byte("Lorem ipsum"))
	w := &bytes.Buffer{}
	io.Copy(w, r)
	fmt.Println(w.String())
	// Output: Lorem ipsum
}
