package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newEidetixSetCmdForTest() *cobra.Command {
	cmd := &cobra.Command{Use: "set"}
	registerEidetixSetFlags(cmd)
	return cmd
}

func TestResolveEidetixToken_Inline(t *testing.T) {
	cmd := newEidetixSetCmdForTest()
	cmd.Flags().Set("token", "fake-token")
	tok, err := resolveEidetixToken(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "fake-token" {
		t.Errorf("token = %q, want fake-token", tok)
	}
}

func TestResolveEidetixToken_Stdin(t *testing.T) {
	cmd := newEidetixSetCmdForTest()
	cmd.Flags().Set("token-stdin", "true")
	cmd.SetIn(strings.NewReader("fake-token-from-stdin\n"))
	tok, err := resolveEidetixToken(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "fake-token-from-stdin" {
		t.Errorf("token = %q, want trimmed stdin value", tok)
	}
}

func TestResolveEidetixToken_MutuallyExclusive(t *testing.T) {
	cmd := newEidetixSetCmdForTest()
	cmd.Flags().Set("token", "a")
	cmd.Flags().Set("token-stdin", "true")
	if _, err := resolveEidetixToken(cmd); err == nil {
		t.Fatalf("expected mutual-exclusion error")
	}
}

func TestResolveEidetixToken_Missing(t *testing.T) {
	cmd := newEidetixSetCmdForTest()
	if _, err := resolveEidetixToken(cmd); err == nil {
		t.Fatalf("expected error when no token source provided")
	}
}
