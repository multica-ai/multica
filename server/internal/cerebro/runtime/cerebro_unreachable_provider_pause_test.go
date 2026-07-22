package runtime

// CEREBRO-PATCH(unreachable-provider-pause): FIR-3651 feedback loop for
// the runtime that cannot reach its provider but keeps claiming tasks.
// Replays the error text produced by Claude (sara.local) on 2026-07-22,
// which failed 14 of its last 15 runs with the same line while the
// runtime stayed online and kept accepting work.

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// unreachableAPIError is the verbatim failure text of those runs.
const unreachableAPIError = "API Error: Unable to connect to API (FailedToOpenSocket)"

// TestUnreachableProviderPausesRuntime pins the user-visible symptom: a
// runtime whose agent process cannot open a socket to the provider must
// be paused so queued work waits, instead of burning both attempts of
// every task that lands on it.
func TestUnreachableProviderPausesRuntime(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	task := db.AgentTaskQueue{
		RuntimeID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true},
		Error:     pgtype.Text{String: unreachableAPIError, Valid: true},
	}

	got := classifyAutoPause(task, now)
	if !got.pauseWorthy {
		t.Fatalf("runtime that cannot reach the provider was not paused: %+v", got)
	}
}
