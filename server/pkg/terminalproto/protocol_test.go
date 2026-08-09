package terminalproto

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestBinaryFrameRoundTrip(t *testing.T) {
	id := uuid.New()
	payload := []byte("\x1b[31mhello\x1b[0m")
	raw, err := EncodeBinary(KindOutput, id, 42, payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeBinary(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindOutput || got.SessionID != id || got.Sequence != 42 || !bytes.Equal(got.Payload, payload) {
		t.Fatalf("round trip = %#v", got)
	}
}

func TestBinaryFrameRejectsOversizedAndMalformedInput(t *testing.T) {
	if _, err := EncodeBinary(KindInput, uuid.New(), 1, make([]byte, MaxPayloadBytes+1)); err == nil {
		t.Fatal("expected oversized payload rejection")
	}
	if _, err := DecodeBinary([]byte("not-a-frame")); err == nil {
		t.Fatal("expected malformed frame rejection")
	}
}
