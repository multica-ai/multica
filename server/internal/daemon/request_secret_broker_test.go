package daemon

import (
	"testing"
	"time"
)

func TestRequestSecretBroker_PutTakeOneShot(t *testing.T) {
	b := newRequestSecretBroker(0)
	b.Put("task-1", "tok-1", "secret-1")
	if b.len() != 1 {
		t.Fatalf("expected 1 entry, got %d", b.len())
	}
	got, ok := b.Take("task-1", "tok-1")
	if !ok || got != "secret-1" {
		t.Fatalf("expected secret-1, got %q ok=%v", got, ok)
	}
	// One-shot: a second take gets nothing.
	if _, ok := b.Take("task-1", "tok-1"); ok {
		t.Fatal("expected one-shot take")
	}
	if b.len() != 0 {
		t.Fatalf("expected empty after take, got %d", b.len())
	}
}

func TestRequestSecretBroker_WrongTokenNotBurned(t *testing.T) {
	b := newRequestSecretBroker(0)
	b.Put("task-1", "tok-1", "secret-1")
	// Wrong token is rejected...
	if _, ok := b.Take("task-1", "wrong-tok"); ok {
		t.Fatal("wrong token must be rejected")
	}
	// ...but the entry survives for the legitimate holder.
	if got, ok := b.Take("task-1", "tok-1"); !ok || got != "secret-1" {
		t.Fatalf("legit token must still take the secret, got %q ok=%v", got, ok)
	}
}

func TestRequestSecretBroker_MissingTask(t *testing.T) {
	b := newRequestSecretBroker(0)
	if _, ok := b.Take("nope", "tok"); ok {
		t.Fatal("missing task must be absent")
	}
}

func TestRequestSecretBroker_Expiry(t *testing.T) {
	b := newRequestSecretBroker(50 * time.Millisecond)
	clock := time.Now()
	b.now = func() time.Time { return clock }
	b.Put("task-1", "tok-1", "secret-1")

	clock = clock.Add(time.Second) // advance past TTL
	if _, ok := b.Take("task-1", "tok-1"); ok {
		t.Fatal("expired entry must be absent")
	}
	if b.len() != 0 {
		t.Fatalf("expired entry must be swept on take, got %d", b.len())
	}
}

func TestRequestSecretBroker_Sweep(t *testing.T) {
	b := newRequestSecretBroker(50 * time.Millisecond)
	clock := time.Now()
	b.now = func() time.Time { return clock }
	b.Put("task-1", "tok-1", "secret-1")
	b.Put("task-2", "tok-2", "secret-2")

	clock = clock.Add(time.Second)
	b.sweep()
	if b.len() != 0 {
		t.Fatalf("sweep must drop expired entries, got %d", b.len())
	}
}

func TestRequestSecretBroker_Discard(t *testing.T) {
	b := newRequestSecretBroker(0)
	b.Put("task-1", "tok-1", "secret-1")
	b.Discard("task-1")
	if _, ok := b.Take("task-1", "tok-1"); ok {
		t.Fatal("discarded entry must be absent")
	}
}

func TestRequestSecretBroker_EmptyInputsAreNoOps(t *testing.T) {
	b := newRequestSecretBroker(0)
	b.Put("", "tok", "secret")  // no task id
	b.Put("task", "", "secret") // no token
	b.Put("task", "tok", "")    // no secret
	if b.len() != 0 {
		t.Fatalf("empty inputs must not stash anything, got %d", b.len())
	}
	if _, ok := b.Take("", "tok"); ok {
		t.Fatal("empty task id take must be absent")
	}
	if _, ok := b.Take("task", ""); ok {
		t.Fatal("empty token take must be absent")
	}
}

// nilBroker exercises the nil-receiver safety of the broker methods.
func TestRequestSecretBroker_NilSafe(t *testing.T) {
	var b *requestSecretBroker
	b.Put("t", "tok", "s")
	b.Discard("t")
	b.sweep()
	if _, ok := b.Take("t", "tok"); ok {
		t.Fatal("nil broker take must be absent")
	}
	if b.len() != 0 {
		t.Fatalf("nil broker len must be 0, got %d", b.len())
	}
}
