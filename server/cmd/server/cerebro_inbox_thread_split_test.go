package main

import (
	"context"
	"strings"
	"testing"
)

func TestCerebroThreadPreview(t *testing.T) {
	if got := cerebroThreadPreview("hello world"); got != "hello world" {
		t.Fatalf("plain content changed: %q", got)
	}
	if got := cerebroThreadPreview("line one\nline two\ttabbed"); got != "line one line two tabbed" {
		t.Fatalf("newlines/tabs not flattened to spaces: %q", got)
	}
	long := strings.Repeat("a", 500)
	got := cerebroThreadPreview(long)
	if len([]rune(got)) != 140 {
		t.Fatalf("preview not capped at 140 runes: got %d", len([]rune(got)))
	}
}

func TestCerebroChannelThreadDetailsLeavesNonChannelUntouched(t *testing.T) {
	existing := []byte(`{"comment_id":"c1"}`)
	// A non-channel issue must be returned unchanged (and never touches the DB,
	// so a nil Queries is safe here).
	got := cerebroChannelThreadDetails(context.Background(), nil, "issue", "ws", "c1", existing)
	if string(got) != string(existing) {
		t.Fatalf("non-channel issue should pass through unchanged, got %q", got)
	}
}

func TestCerebroChannelThreadDetailsLeavesInvalidIDsUntouched(t *testing.T) {
	existing := []byte(`{"comment_id":"c1"}`)
	// channel/dm kind but an unparseable comment id → return before any DB call.
	got := cerebroChannelThreadDetails(context.Background(), nil, "channel", "ws", "not-a-uuid", existing)
	if string(got) != string(existing) {
		t.Fatalf("invalid comment id should pass through unchanged, got %q", got)
	}
	// Valid comment id but unparseable workspace id → return before any DB call.
	got = cerebroChannelThreadDetails(context.Background(), nil, "dm", "not-a-uuid", "11111111-1111-1111-1111-111111111111", existing)
	if string(got) != string(existing) {
		t.Fatalf("invalid workspace id should pass through unchanged, got %q", got)
	}
}
