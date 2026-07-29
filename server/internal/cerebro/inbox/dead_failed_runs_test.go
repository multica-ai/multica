package inbox

// FIR-3901 — the Resume button must never promise something the daemon will
// not do. These cover the explanation the API hands the UI when resume is off.

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func TestResumeBlockedReason(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		task cerebrodb.DeadFailedTask
		want string
	}{
		{
			name: "resumable runs carry no explanation",
			task: cerebrodb.DeadFailedTask{ResumePossible: true, HasSession: true, RuntimeOnline: true},
			want: "",
		},
		{
			name: "no conversation is reported before anything else",
			task: cerebrodb.DeadFailedTask{HasSession: false, RuntimeOnline: false},
			want: "never got far enough",
		},
		{
			name: "a poisoned conversation says continuing would fail again",
			task: cerebrodb.DeadFailedTask{HasSession: true, RuntimeOnline: true, FailureReason: text("api_invalid_request")},
			want: "would fail the same way",
		},
		{
			name: "an offline machine names the machine",
			task: cerebrodb.DeadFailedTask{HasSession: true, RuntimeOnline: false, RuntimeName: text("Claude (sara.local)")},
			want: "Claude (sara.local) is offline",
		},
		{
			name: "an offline machine with no name still explains itself",
			task: cerebrodb.DeadFailedTask{HasSession: true, RuntimeOnline: false},
			want: "The machine is offline",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resumeBlockedReason(tc.task)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("expected no explanation, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("resumeBlockedReason() = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

// The blacklist must stay in lock-step with GetLastTaskSession's NOT IN list
// and daemon/poisoned.go. Drift here means the UI offers Resume on a run the
// daemon will silently start blank.
func TestPoisonedReasonsMatchResumeLookup(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{
		"iteration_limit",
		"agent_fallback_message",
		"api_invalid_request",
		"codex_semantic_inactivity",
	} {
		if !isPoisonedReason(reason) {
			t.Errorf("%q must be treated as poisoned", reason)
		}
	}
	for _, reason := range []string{
		"runtime_offline",
		"runtime_recovery",
		"agent_error.unknown",
		"timeout",
		"",
	} {
		if isPoisonedReason(reason) {
			t.Errorf("%q must NOT be treated as poisoned — it is resumable", reason)
		}
	}
}

// FIR-4073 — a run whose machine is merely paused must be reported as paused,
// with the time it comes back, so the UI shows a grey "waiting" row instead of
// a red "this failed" one. That row is what replaces the auto-pause comment.
func TestToDeadFailedResponsePausedRuntime(t *testing.T) {
	back := time.Date(2026, 7, 29, 16, 0, 0, 0, time.UTC)

	got := toDeadFailedResponse(cerebrodb.DeadFailedTask{
		FailureReason: pgtype.Text{String: "runtime_paused", Valid: true},
		RuntimeName:   pgtype.Text{String: "sara-mac", Valid: true},
		RuntimePaused: true,
		UnpauseAt:     pgtype.Timestamptz{Time: back, Valid: true},
	})

	if !got.RuntimePaused {
		t.Fatal("a run on a paused machine must report runtime_paused")
	}
	if got.UnpauseAt != "2026-07-29T16:00:00Z" {
		t.Fatalf("unpause_at = %q, want the machine's return time", got.UnpauseAt)
	}
}

// A pause the circuit breaker gave up on has no return time. The empty value is
// the signal the UI turns into "needs a human", so it must not be invented.
func TestToDeadFailedResponsePausedWithoutReturnTime(t *testing.T) {
	got := toDeadFailedResponse(cerebrodb.DeadFailedTask{RuntimePaused: true})

	if !got.RuntimePaused {
		t.Fatal("still a paused run")
	}
	if got.UnpauseAt != "" {
		t.Fatalf("unpause_at = %q, want empty when auto-resume gave up", got.UnpauseAt)
	}
}

// An ordinary failure must stay red — no paused fields leak into it.
func TestToDeadFailedResponseOrdinaryFailureIsNotPaused(t *testing.T) {
	got := toDeadFailedResponse(cerebrodb.DeadFailedTask{
		FailureReason:  pgtype.Text{String: "runtime_offline", Valid: true},
		ResumePossible: true,
	})

	if got.RuntimePaused || got.UnpauseAt != "" {
		t.Fatalf("ordinary failure reported as paused: %+v", got)
	}
}
