package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func staged(stage int32, closed, cancelled bool) stagedChild {
	ch := stagedChild{closed: closed, cancelled: cancelled}
	if stage > 0 {
		ch.issue = db.Issue{Stage: pgtype.Int4{Int32: stage, Valid: true}}
	}
	return ch
}

// A stage that closed with cancelled work halts dispatch instead of opening
// the next one; cancelled work in a stage still open does not.
func TestOpenStage(t *testing.T) {
	cases := []struct {
		name     string
		children []stagedChild
		want     int32 // 0: nothing to dispatch
	}{
		{"first stage open", []stagedChild{staged(1, false, false), staged(2, false, false)}, 1},
		{"stage 1 done, stage 2 opens", []stagedChild{staged(1, true, false), staged(2, false, false)}, 2},
		{"stage 1 closed with a cancellation halts", []stagedChild{staged(1, true, false), staged(1, true, true), staged(2, false, false)}, 0},
		{"cancellation in a stage still open", []stagedChild{staged(1, true, true), staged(1, false, false), staged(2, false, false)}, 1},
		{"unstaged ignored", []stagedChild{staged(0, false, false), staged(1, true, false)}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := int32(0)
			if s := openStage(tc.children); s != nil {
				got = *s
			}
			if got != tc.want {
				t.Fatalf("openStage = %d, want %d", got, tc.want)
			}
		})
	}
}

// The child_done wake for a closed stage is skipped under stage dispatch only
// when the stages it closes hold no cancelled work.
func TestStageCancelled(t *testing.T) {
	cases := []struct {
		name     string
		children []subIssue
		want     bool
	}{
		{"all done", []subIssue{sub(1, true, false), sub(2, false, false)}, false},
		{"closed stage cancelled", []subIssue{sub(1, true, false), sub(1, true, true), sub(2, false, false)}, true},
		{"earlier stage cancelled", []subIssue{sub(1, true, true), sub(2, true, false), sub(3, false, false)}, true},
		{"only a later stage cancelled", []subIssue{sub(1, true, false), sub(2, true, true), sub(2, false, false)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			met, _, observed := stageProgress(tc.children)
			if !met {
				t.Fatalf("stageProgress not met: %v", observed)
			}
			if got := stageCancelled(observed); got != tc.want {
				t.Fatalf("stageCancelled = %v, want %v (%v)", got, tc.want, observed)
			}
		})
	}
}
