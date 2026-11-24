package js

import (
	"testing"

	"github.com/tdewolff/test"
)

func TestAsIdentifierName(t *testing.T) {
	test.That(t, !AsIdentifierName([]byte("")))
	test.That(t, !AsIdentifierName([]byte("5")))
	test.That(t, AsIdentifierName([]byte("ab")))
	test.That(t, !AsIdentifierName([]byte("a=")))
}

func TestAsDecimalLiteral(t *testing.T) {
	test.That(t, !AsDecimalLiteral([]byte("")))
	test.That(t, !AsDecimalLiteral([]byte("a")))
	test.That(t, AsDecimalLiteral([]byte("12")))
	test.That(t, AsDecimalLiteral([]byte("12.56")))
	test.That(t, AsDecimalLiteral([]byte(".56")))
	test.That(t, !AsDecimalLiteral([]byte(".56a")))
	test.That(t, !AsDecimalLiteral([]byte(".")))
	test.That(t, AsDecimalLiteral([]byte("0")))
	test.That(t, !AsDecimalLiteral([]byte("00")))
}
