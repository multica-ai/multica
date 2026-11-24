package buffer

import (
	"bytes"
	"io"
	"testing"

	"github.com/tdewolff/test"
)

func TestLexer(t *testing.T) {
	s := `Lorem ipsum dolor sit amet, consectetur adipiscing elit.`
	z := NewLexer(bytes.NewBufferString(s))

	test.Bytes(t, z.Bytes(), []byte(s), "bytes match original buffer")

	test.T(t, z.err, nil, "buffer has no error")
	test.T(t, z.Err(), nil, "buffer is at EOF but must not return EOF until we reach that")
	test.That(t, z.Pos() == 0, "buffer must start at position 0")
	test.That(t, z.Peek(0) == 'L', "first character must be 'L'")
	test.That(t, z.Peek(1) == 'o', "second character must be 'o'")

	z.Move(1)
	test.That(t, z.Peek(0) == 'o', "must be 'o' at position 1")
	test.That(t, z.Peek(1) == 'r', "must be 'r' at position 1")
	z.Rewind(6)
	test.That(t, z.Peek(0) == 'i', "must be 'i' at position 6")
	test.That(t, z.Peek(1) == 'p', "must be 'p' at position 7")

	test.T(t, z.Offset(), 6, "offset")

	test.Bytes(t, z.Lexeme(), []byte("Lorem "), "buffered string must now read 'Lorem ' when at position 6")
	test.Bytes(t, z.Shift(), []byte("Lorem "), "shift must return the buffered string")
	test.That(t, z.Pos() == 0, "after shifting position must be 0")
	test.That(t, z.Peek(0) == 'i', "must be 'i' at position 0 after shifting")
	test.That(t, z.Peek(1) == 'p', "must be 'p' at position 1 after shifting")
	test.T(t, z.Err(), nil, "error must be nil at this point")

	z.Move(len(s) - len("Lorem ") - 1)
	test.T(t, z.Err(), nil, "error must be nil just before the end of the buffer")
	z.Skip()
	test.That(t, z.Pos() == 0, "after skipping position must be 0")
	z.Move(1)
	test.T(t, z.Err(), io.EOF, "error must be EOF when past the buffer")
	z.Move(-1)
	test.T(t, z.Err(), nil, "error must be nil just before the end of the buffer, even when it has been past the buffer")

	z.Reset()
	test.That(t, z.Peek(0) == 'L', "must be 'L' at position 0")
	test.That(t, z.Peek(1) == 'o', "must be 'o' at position 1")
}

func TestLexerRunes(t *testing.T) {
	z := NewLexer(bytes.NewBufferString("aæ†\U00100000"))
	r, n := z.PeekRune(0)
	test.That(t, n == 1, "first character must be length 1")
	test.That(t, r == 'a', "first character must be rune 'a'")
	r, n = z.PeekRune(1)
	test.That(t, n == 2, "second character must be length 2")
	test.That(t, r == 'æ', "second character must be rune 'æ'")
	r, n = z.PeekRune(3)
	test.That(t, n == 3, "fourth character must be length 3")
	test.That(t, r == '†', "fourth character must be rune '†'")
	r, n = z.PeekRune(6)
	test.That(t, n == 4, "seventh character must be length 4")
	test.That(t, r == '\U00100000', "seventh character must be rune '\U00100000'")
}

func TestLexerBadRune(t *testing.T) {
	z := NewLexer(bytes.NewBufferString("\xF0")) // expect four byte rune
	r, n := z.PeekRune(0)
	test.T(t, n, 1, "length")
	test.T(t, r, rune(0xF0), "rune")
}

func TestLexerZeroLen(t *testing.T) {
	z := NewLexer(test.NewPlainReader(bytes.NewBufferString("")))
	test.That(t, z.Peek(0) == 0, "first character must yield error")
	test.Bytes(t, z.Bytes(), []byte{}, "bytes match original buffer")
}

func TestLexerEmptyReader(t *testing.T) {
	z := NewLexer(test.NewEmptyReader())
	test.That(t, z.Peek(0) == 0, "first character must yield error")
	test.T(t, z.Err(), io.EOF, "error must be EOF")
	test.That(t, z.Peek(0) == 0, "second peek must also yield error")
}

func TestLexerErrorReader(t *testing.T) {
	z := NewLexer(test.NewErrorReader(0))
	test.That(t, z.Peek(0) == 0, "first character must yield error")
	test.T(t, z.Err(), test.ErrPlain, "error must be ErrPlain")
	test.That(t, z.Peek(0) == 0, "second peek must also yield error")
}

func TestLexerBytes(t *testing.T) {
	b := []byte{'t', 'e', 's', 't'}
	z := NewLexerBytes(b)
	test.That(t, z.Peek(4) == 0, "fifth character must yield NULL")
}

func TestLexerRestore(t *testing.T) {
	b := []byte{'a', 'b', 'c', 'd'}
	z := NewLexerBytes(b[:2])

	test.T(t, len(z.buf), 3, "must have terminating NULL")
	test.T(t, z.buf[2], byte(0), "must have terminating NULL")
	test.Bytes(t, b, []byte{'a', 'b', 0, 'd'}, "terminating NULL overwrites underlying buffer")

	z.Restore()
	test.Bytes(t, b, []byte{'a', 'b', 'c', 'd'}, "terminating NULL has been restored")
}
