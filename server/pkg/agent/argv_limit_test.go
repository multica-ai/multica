package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"
	"testing"
)

func TestWrapArgvLimitErrorNamesE2BIG(t *testing.T) {
	t.Parallel()

	// The shape os/exec returns for the #9362 incident.
	raw := &fs.PathError{Op: "fork/exec", Path: "/usr/bin/copilot", Err: syscall.E2BIG}
	got := wrapArgvLimitError(raw)

	if !errors.Is(got, ErrArgvLimitExceeded) {
		t.Fatalf("expected ErrArgvLimitExceeded on the chain, got %v", got)
	}
	if !errors.Is(got, syscall.E2BIG) {
		t.Fatalf("raw errno must stay on the chain, got %v", got)
	}
	if !strings.Contains(got.Error(), "attachments") {
		t.Fatalf("expected the remedy in the message, got %q", got.Error())
	}
}

func TestWrapArgvLimitErrorPassesOtherErrorsThrough(t *testing.T) {
	t.Parallel()

	raw := fmt.Errorf("start: %w", syscall.ENOENT)
	if got := wrapArgvLimitError(raw); got != raw {
		t.Fatalf("unrelated error was rewritten: %v", got)
	}
	if wrapArgvLimitError(nil) != nil {
		t.Fatal("nil must stay nil")
	}
}
