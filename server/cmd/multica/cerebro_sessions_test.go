// CEREBRO-PATCH(cerebro-sessions-cli): FIR-3565 cerebro-only file — the handoff
// brief flags must take their value verbatim. They used to be StringSlice, which
// splits on every comma and cut a single prose sentence into several bullets.
package main

import (
	"testing"

	"github.com/spf13/cobra"
)

const handoffSentence = "Rewrote the runner, added tests, and verified registry still passes"

// Each test gets its own command so flag values never leak between tests.
func parseHandoffFlags(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "handoff"}
	registerIssueSessionHandoffFlags(cmd)
	if err := cmd.Flags().Parse(args); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestHandoffDoneKeepsCommasInOneItem(t *testing.T) {
	cmd := parseHandoffFlags(t, "--done", handoffSentence)
	done, err := cmd.Flags().GetStringArray("done")
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0] != handoffSentence {
		t.Errorf("--done = %#v, want one intact sentence", done)
	}
}

func TestHandoffRemainingKeepsCommasInOneItem(t *testing.T) {
	cmd := parseHandoffFlags(t, "--remaining", handoffSentence)
	remaining, err := cmd.Flags().GetStringArray("remaining")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0] != handoffSentence {
		t.Errorf("--remaining = %#v, want one intact sentence", remaining)
	}
}

func TestHandoffRepeatedFlagStillYieldsSeveralItems(t *testing.T) {
	cmd := parseHandoffFlags(t, "--done", "First, with a comma", "--done", "Second")
	done, err := cmd.Flags().GetStringArray("done")
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 2 || done[0] != "First, with a comma" || done[1] != "Second" {
		t.Errorf("--done = %#v, want two items with the comma preserved", done)
	}
}
