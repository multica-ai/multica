package main

import (
	"strings"
	"testing"
)

func TestDefaultCommentIdempotencyKey(t *testing.T) {
	first := defaultCommentIdempotencyKey("task-1", "issue-1", "parent-1", "same content", false)
	retry := defaultCommentIdempotencyKey("task-1", "issue-1", "parent-1", "same content", false)
	if first != retry || !strings.HasPrefix(first, "task-comment-v1:") {
		t.Fatalf("task retry keys = %q and %q, want same deterministic key", first, retry)
	}
	changed := defaultCommentIdempotencyKey("task-1", "issue-1", "parent-1", "different content", false)
	if changed == first {
		t.Fatal("different content must produce a different key")
	}
	withoutTaskA := defaultCommentIdempotencyKey("", "issue-1", "", "same", false)
	withoutTaskB := defaultCommentIdempotencyKey("", "issue-1", "", "same", false)
	if withoutTaskA == withoutTaskB {
		t.Fatal("interactive comments without task identity must get distinct keys")
	}
}
