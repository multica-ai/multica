package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A triage run is not retried (MUL-7189 §5.6). Re-triaging reads the entry as
// it stands now, so replaying a failed attempt is never the recovery wanted.
func TestRetryEligibleExcludesTriageRuns(t *testing.T) {
	issueLinked := func(context string) db.AgentTaskQueue {
		return db.AgentTaskQueue{
			Attempt:     1,
			MaxAttempts: 2,
			IssueID:     pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
			Context:     []byte(context),
		}
	}
	cases := map[string]struct {
		context string
		want    bool
	}{
		// The baseline: without the marker this reason/attempt pair retries, so
		// a `false` below is the marker's doing and nothing else.
		"no context":           {"", true},
		"unrelated context":    {`{"head_sha":"abc123"}`, true},
		"other context type":   {`{"type":"quick_create"}`, true},
		"triage run":           {`{"type":"triage"}`, false},
		"triage with revision": {`{"type":"triage","triage_revision":3}`, false},
		// A context that is not an object cannot claim to be a triage run, and
		// must not make the whole eligibility check fall over either.
		"malformed context": {`not json`, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := retryEligible("timeout", issueLinked(tc.context)); got != tc.want {
				t.Errorf("retryEligible with context %q = %v, want %v", tc.context, got, tc.want)
			}
		})
	}
}
