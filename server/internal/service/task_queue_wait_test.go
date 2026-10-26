package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Pure mapping/diff coverage for queue-wait attribution. Runs without a
// database; the DB-backed lifecycle (write, clear, poll integration) lives in
// task_queue_wait_db_test.go.

func waitRow(id pgtype.UUID, reason string) db.AgentTaskQueue {
	row := db.AgentTaskQueue{ID: id}
	if reason != "" {
		row.QueueWaitReason = pgtype.Text{String: reason, Valid: true}
	}
	return row
}

func TestQueueWaitUpdatesForAgent(t *testing.T) {
	head := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	tail := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	candidates := []db.AgentTaskQueue{waitRow(head, ""), waitRow(tail, "")}

	t.Run("no_capacity attributes every candidate row of the agent", func(t *testing.T) {
		got := queueWaitUpdatesForAgent(candidates, claimAttempt{Outcome: claimOutcomeNoCapacity})
		if len(got) != 2 {
			t.Fatalf("updates = %v, want 2", got)
		}
		for i, want := range []pgtype.UUID{head, tail} {
			if got[i].taskID != want || got[i].reason != queueWaitAgentCapacity {
				t.Fatalf("update[%d] = %+v, want {%v %s}", i, got[i], want, queueWaitAgentCapacity)
			}
		}
	})

	t.Run("no_tasks attributes only the head candidate", func(t *testing.T) {
		got := queueWaitUpdatesForAgent(candidates, claimAttempt{Outcome: claimOutcomeNoTasks})
		if len(got) != 1 || got[0].taskID != head || got[0].reason != queueWaitAttemptedUnclaimed {
			t.Fatalf("updates = %+v, want head-only %s", got, queueWaitAttemptedUnclaimed)
		}
	})

	t.Run("binding outcomes attribute every candidate row", func(t *testing.T) {
		for outcome, reason := range map[string]string{
			claimOutcomeRuntimeMismatch: queueWaitAgentRebound,
			claimOutcomeNoRuntime:       queueWaitAgentUnbound,
		} {
			got := queueWaitUpdatesForAgent(candidates, claimAttempt{Outcome: outcome})
			if len(got) != 2 {
				t.Fatalf("outcome %s: updates = %v, want 2", outcome, got)
			}
			if got[0].reason != reason {
				t.Fatalf("outcome %s: reason = %q, want %q", outcome, got[0].reason, reason)
			}
		}
	})

	t.Run("claimed, errors, and empty attempts write nothing", func(t *testing.T) {
		for _, outcome := range []string{claimOutcomeClaimed, claimOutcomeErrorClaim, "unknown", ""} {
			if got := queueWaitUpdatesForAgent(candidates, claimAttempt{Outcome: outcome}); got != nil {
				t.Fatalf("outcome %q: updates = %v, want nil", outcome, got)
			}
		}
	})

	t.Run("steady state: rows already carrying the reason produce no write", func(t *testing.T) {
		// Both rows carry the value the poll is about to persist — the poll
		// must not even build an update, which is what keeps the idle-but-
		// waiting path write-free poll after poll.
		same := []db.AgentTaskQueue{waitRow(head, queueWaitAgentCapacity), waitRow(tail, queueWaitAgentCapacity)}
		if got := queueWaitUpdatesForAgent(same, claimAttempt{Outcome: claimOutcomeNoCapacity}); len(got) != 0 {
			t.Fatalf("updates = %v, want none", got)
		}
	})

	t.Run("partial steady state: only changed rows are written", func(t *testing.T) {
		mixed := []db.AgentTaskQueue{waitRow(head, queueWaitAgentCapacity), waitRow(tail, "")}
		got := queueWaitUpdatesForAgent(mixed, claimAttempt{Outcome: claimOutcomeNoCapacity})
		if len(got) != 1 || got[0].taskID != tail {
			t.Fatalf("updates = %+v, want tail only", got)
		}
	})

	t.Run("reason transitions rewrite the row", func(t *testing.T) {
		changed := []db.AgentTaskQueue{waitRow(head, queueWaitAttemptedUnclaimed)}
		got := queueWaitUpdatesForAgent(changed, claimAttempt{Outcome: claimOutcomeNoCapacity})
		if len(got) != 1 || got[0].reason != queueWaitAgentCapacity {
			t.Fatalf("updates = %+v, want one agent_capacity write", got)
		}
	})

	t.Run("no candidates means no updates", func(t *testing.T) {
		if got := queueWaitUpdatesForAgent(nil, claimAttempt{Outcome: claimOutcomeNoCapacity}); got != nil {
			t.Fatalf("updates = %v, want nil", got)
		}
	})
}
