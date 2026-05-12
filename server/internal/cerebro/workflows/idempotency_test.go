package workflows

import "testing"

func TestIdempotencyKey_Stable(t *testing.T) {
	a := IdempotencyKey("evt-1", "wf-1")
	b := IdempotencyKey("evt-1", "wf-1")
	if a != b {
		t.Fatalf("same inputs must produce same key: %q vs %q", a, b)
	}
}

func TestIdempotencyKey_DistinguishesArguments(t *testing.T) {
	// The pair ("ab", "c") vs ("a", "bc") used to collide before we added
	// the explicit separator byte. Pin this so a future refactor can't
	// silently reintroduce the bug.
	a := IdempotencyKey("ab", "c")
	b := IdempotencyKey("a", "bc")
	if a == b {
		t.Fatalf("separator missing — %q must differ from %q", a, b)
	}
}

func TestIdempotencyKey_Different(t *testing.T) {
	cases := []struct {
		event, action string
	}{
		{"evt-1", "wf-2"},
		{"evt-2", "wf-1"},
		{"", "wf-1"},
		{"evt-1", ""},
	}
	base := IdempotencyKey("evt-1", "wf-1")
	for _, c := range cases {
		if got := IdempotencyKey(c.event, c.action); got == base {
			t.Errorf("IdempotencyKey(%q,%q) collided with the base key", c.event, c.action)
		}
	}
}
