package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestCursorConnectionStalledRetriesWithSession(t *testing.T) {
	t.Parallel()
	const raw = "cursor-agent exited with error: exit status 1 (result_seen=false, exit_code=1, scanner_error=false, event_count=463, invalid_event_count=0, last_event_type=system); actions completed before finalization may already have taken effect; cursor stderr: RetriableError: Connection stalled"
	for _, reported := range []string{"", "agent_error.process_failure", "agent_error.unknown", "agent_error"} {
		t.Run(reported, func(t *testing.T) {
			reason := reported
			if reason == "" {
				reason = taskfailure.Classify(raw).String()
			}
			reason = taskfailure.NormalizeDaemonReason(reason, raw).String()
			if reason != taskfailure.ReasonAgentProviderNetwork.String() {
				t.Fatalf("resolved reason = %q, want provider_network", reason)
			}
			if resumeUnsafeFailureReason(reason) {
				t.Fatal("stalled transport must preserve the session for retry")
			}
			for _, tc := range []struct {
				name         string
				attempt, max int32
				want         bool
			}{
				{"first failure", 1, 2, true},
				{"second failure", 2, 2, true},
				{"retry ceiling", providerNetworkMaxAttempts, 2, false},
				{"explicitly disabled", 1, 1, false},
			} {
				task := db.AgentTaskQueue{
					Attempt: tc.attempt, MaxAttempts: tc.max,
					IssueID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
				}
				if got := retryEligible(reason, task); got != tc.want {
					t.Errorf("%s: retryEligible = %v, want %v", tc.name, got, tc.want)
				}
			}
		})
	}
}
