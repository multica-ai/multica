package dbid

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestNewV7ReturnsAValidVersion7UUID(t *testing.T) {
	got := NewV7()
	if !got.Valid {
		t.Fatal("NewV7 returned an invalid pgtype.UUID; the insert would fall back to the DB default")
	}
	parsed := uuid.UUID(got.Bytes)
	if v := parsed.Version(); v != 7 {
		t.Fatalf("uuid version = %d, want 7 (%s)", v, parsed)
	}
	if vr := parsed.Variant(); vr != uuid.RFC4122 {
		t.Fatalf("uuid variant = %v, want RFC4122 (%s)", vr, parsed)
	}
}

func TestNewV7IsUniqueAndNonDecreasing(t *testing.T) {
	const n = 500

	seen := make(map[uuid.UUID]struct{}, n)
	var prev []byte
	for i := 0; i < n; i++ {
		id := NewV7()
		if !id.Valid {
			t.Fatalf("call %d returned an invalid uuid", i)
		}
		parsed := uuid.UUID(id.Bytes)
		if _, dup := seen[parsed]; dup {
			t.Fatalf("call %d produced a duplicate id %s", i, parsed)
		}
		seen[parsed] = struct{}{}

		// The first 6 bytes are the big-endian millisecond timestamp. Within one
		// process they must never go backwards, which is the whole point of using
		// v7 for a primary key: byte order tracks insert order, so consecutive
		// rows land on the same B-tree page.
		ts := id.Bytes[:6]
		if prev != nil && bytes.Compare(ts, prev) < 0 {
			t.Fatalf("call %d timestamp prefix went backwards: %x < %x", i, ts, prev)
		}
		prev = append(prev[:0:0], ts...)
	}
}
