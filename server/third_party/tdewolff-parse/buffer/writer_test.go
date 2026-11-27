package buffer

import (
	"fmt"
	"testing"

	"github.com/tdewolff/test"
)

func TestWriter(t *testing.T) {
	w := NewWriter(make([]byte, 0, 3))

	test.That(t, w.Len() == 0, "buffer must initially have zero length")

	n, _ := w.Write([]byte("abc"))
	test.That(t, n == 3, "first write must write 3 characters")
	test.Bytes(t, w.Bytes(), []byte("abc"), "first write must match 'abc'")
	test.That(t, w.Len() == 3, "buffer must have length 3 after first write")

	n, _ = w.Write([]byte("def"))
	test.That(t, n == 3, "second write must write 3 characters")
	test.Bytes(t, w.Bytes(), []byte("abcdef"), "second write must match 'abcdef'")

	w.Reset()
	test.Bytes(t, w.Bytes(), []byte(""), "reset must match ''")

	n, _ = w.Write([]byte("ghijkl"))
	test.That(t, n == 6, "third write must write 6 characters")
	test.Bytes(t, w.Bytes(), []byte("ghijkl"), "third write must match 'ghijkl'")
}

func ExampleNewWriter() {
	w := NewWriter(make([]byte, 0, 11)) // initial buffer length is 11
	w.Write([]byte("Lorem ipsum"))
	fmt.Println(string(w.Bytes()))
	// Output: Lorem ipsum
}

func ExampleWriter_Reset() {
	w := NewWriter(make([]byte, 0, 11))                 // initial buffer length is 10
	w.Write([]byte("garbage that will be overwritten")) // does reallocation
	w.Reset()
	w.Write([]byte("Lorem ipsum"))
	fmt.Println(string(w.Bytes()))
	// Output: Lorem ipsum
}
