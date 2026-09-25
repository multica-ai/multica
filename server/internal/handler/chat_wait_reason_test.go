package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestWaitReasonForStatus pins the gate that keeps a finished hold from being
// reported as a live one. wait_reason is written once, on the way into
// waiting_local_directory, and never cleared — so the status is the only thing
// that says whether the text still describes the present.
func TestWaitReasonForStatus(t *testing.T) {
	t.Parallel()

	held := pgtype.Text{String: "NuvioTV (held by task a1b2c3d4)", Valid: true}

	t.Run("returned while the task is parked", func(t *testing.T) {
		if got := waitReasonForStatus("waiting_local_directory", held); got != "NuvioTV (held by task a1b2c3d4)" {
			t.Fatalf("wait reason = %q, want the stored text", got)
		}
	})

	t.Run("suppressed once the task moves on", func(t *testing.T) {
		for _, status := range []string{"running", "dispatched", "queued", "deferred"} {
			if got := waitReasonForStatus(status, held); got != "" {
				t.Errorf("status %q leaked a stale hold reason: %q", status, got)
			}
		}
	})

	t.Run("absent reason is empty, not NULL text", func(t *testing.T) {
		if got := waitReasonForStatus("waiting_local_directory", pgtype.Text{}); got != "" {
			t.Fatalf("wait reason = %q, want empty for a NULL column", got)
		}
	})

	t.Run("whitespace-only reason reads as absent", func(t *testing.T) {
		blank := pgtype.Text{String: "   ", Valid: true}
		if got := waitReasonForStatus("waiting_local_directory", blank); got != "" {
			t.Fatalf("wait reason = %q, want empty", got)
		}
	})
}
