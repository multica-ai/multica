package inbox

// FIR-3901 — the Resume button must never promise something the daemon will
// not do. These cover the explanation the API hands the UI when resume is off.

import (
	"strings"
	"testing"

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
