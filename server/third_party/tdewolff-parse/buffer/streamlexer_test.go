package buffer

import (
	"bytes"
	"io"
	"testing"

	"github.com/tdewolff/test"
)

func TestBufferPool(t *testing.T) {
	z := &bufferPool{}

	lorem := []byte("Lorem ipsum")
	dolor := []byte("dolor sit amet")
	consectetur := []byte("consectetur adipiscing elit")

	// set lorem as first buffer and get new dolor buffer
	b := z.swap(lorem, len(dolor))
	test.That(t, len(b) == 0)
	test.That(t, cap(b) == len(dolor))
	b = append(b, dolor...)

	// free first buffer so it will be reused
	z.free(len(lorem))
	b = z.swap(b, len(lorem))
	b = b[:len(lorem)]
	test.Bytes(t, b, lorem)

	b = z.swap(b, len(consectetur))
	b = append(b, consectetur...)

	// free in advance to reuse the same buffer
	z.free(len(dolor) + len(lorem) + len(consectetur))
	test.That(t, z.head == 0)
	b = z.swap(b, len(consectetur))
	b = b[:len(consectetur)]
	test.Bytes(t, b, consectetur)

	// free in advance but request larger buffer
	z.free(len(consectetur))
	b = z.swap(b, len(consectetur)+1)
	b = append(b, consectetur...)
	b = append(b, '.')
	test.That(t, cap(b) == len(consectetur)+1)
}

func TestStreamLexer(t *testing.T) {
	s := `Lorem ipsum dolor sit amet, consectetur adipiscing elit.`
	z := NewStreamLexer(bytes.NewBufferString(s))

	test.T(t, z.err, io.EOF, "buffer must be fully in memory")
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

	test.Bytes(t, z.Lexeme(), []byte("Lorem "), "buffered string must now read 'Lorem ' when at position 6")
	test.Bytes(t, z.Shift(), []byte("Lorem "), "shift must return the buffered string")
	test.That(t, z.ShiftLen() == len("Lorem "), "shifted length must equal last shift")
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
	z.Free(0) // has already been tested
}

func TestStreamLexerShift(t *testing.T) {
	s := `Lorem ipsum dolor sit amet, consectetur adipiscing elit.`
	z := NewStreamLexerSize(test.NewPlainReader(bytes.NewBufferString(s)), 5)

	z.Move(len("Lorem "))
	test.Bytes(t, z.Shift(), []byte("Lorem "), "shift must return the buffered string")
	test.That(t, z.ShiftLen() == len("Lorem "), "shifted length must equal last shift")
}

func TestStreamLexerSmall(t *testing.T) {
	s := `abcdefghijklm`
	z := NewStreamLexerSize(test.NewPlainReader(bytes.NewBufferString(s)), 4)
	test.That(t, z.Peek(8) == 'i', "first character must be 'i' at position 8")

	z = NewStreamLexerSize(test.NewPlainReader(bytes.NewBufferString(s)), 4)
	test.That(t, z.Peek(12) == 'm', "first character must be 'm' at position 12")

	z = NewStreamLexerSize(test.NewPlainReader(bytes.NewBufferString(s)), 0)
	test.That(t, z.Peek(4) == 'e', "first character must be 'e' at position 4")

	z = NewStreamLexerSize(test.NewPlainReader(bytes.NewBufferString(s)), 13)
	test.That(t, z.Peek(13) == 0, "must yield error at position 13")
}

func TestStreamLexerSingle(t *testing.T) {
	z := NewStreamLexer(test.NewInfiniteReader())
	test.That(t, z.Peek(0) == '.')
	test.That(t, z.Peek(1) == '.')
	test.That(t, z.Peek(3) == '.', "required two successful reads")
}

func TestStreamLexerRunes(t *testing.T) {
	z := NewStreamLexer(bytes.NewBufferString("aæ†\U00100000"))
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

func TestStreamLexerBadRune(t *testing.T) {
	z := NewStreamLexer(bytes.NewBufferString("\xF0")) // expect four byte rune
	r, n := z.PeekRune(0)
	test.T(t, n, 4, "length")
	test.T(t, r, rune(0), "rune")
}

func TestStreamLexerZeroLen(t *testing.T) {
	z := NewStreamLexer(test.NewPlainReader(bytes.NewBufferString("")))
	test.That(t, z.Peek(0) == 0, "first character must yield error")
}

func TestStreamLexerEmptyReader(t *testing.T) {
	z := NewStreamLexer(test.NewEmptyReader())
	test.That(t, z.Peek(0) == 0, "first character must yield error")
	test.T(t, z.Err(), io.EOF, "error must be EOF")
	test.That(t, z.Peek(0) == 0, "second peek must also yield error")
}
