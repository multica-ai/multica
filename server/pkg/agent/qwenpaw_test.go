package agent

import (
	"testing"
)

func TestQwenpawBlockedArgsProtectsAcpSubcommand(t *testing.T) {
	t.Parallel()
	if mode, ok := qwenpawBlockedArgs["acp"]; !ok {
		t.Fatal("expected 'acp' to be in qwenpawBlockedArgs")
	} else if mode != blockedStandalone {
		t.Fatalf("expected blockedStandalone for 'acp', got %v", mode)
	}
}

func TestNewReturnsQwenpawBackend(t *testing.T) {
	t.Parallel()
	b, err := New("qwenpaw", Config{ExecutablePath: "/nonexistent/qwenpaw"})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}
	if _, ok := b.(*qwenpawBackend); !ok {
		t.Fatalf("expected *qwenpawBackend, got %T", b)
	}
}

func TestNewQwenpawUnknownType(t *testing.T) {
	t.Parallel()
	_, err := New("qwenpaw-nonexistent", Config{})
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}