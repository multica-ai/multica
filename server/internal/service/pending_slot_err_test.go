package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestPendingSlotTakenErr_RecognizesBothShapes pins the two error shapes that can
// reach RerunIssue's reclaim branch. The issue-assignee enqueue surfaces the raw
// unique violation, but enqueueMentionTaskWithCommentPlan normalizes it into the
// bare ErrDuplicatePendingTask sentinel — and that is the path EVERY rerun takes
// whose target is not the issue's current agent assignee (squad leader, an agent
// re-fired by task_id after being displaced, a mentioned agent).
//
// Matching only the pgconn shape silently excluded all of those from the reclaim,
// so a system retry winning the slot surfaced to the operator as a hard error.
func TestPendingSlotTakenErr_RecognizesBothShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "raw unique violation from the issue-assignee path",
			err:  &pgconn.PgError{Code: "23505", ConstraintName: "idx_one_pending_task_per_issue_agent_v2"},
			want: true,
		},
		{
			name: "legacy v1 index name during a rolling deploy",
			err:  &pgconn.PgError{Code: "23505", ConstraintName: "idx_one_pending_task_per_issue_agent"},
			want: true,
		},
		{
			name: "normalized sentinel from the mention path",
			err:  ErrDuplicatePendingTask,
			want: true,
		},
		{
			name: "wrapped sentinel",
			err:  fmt.Errorf("enqueue rerun: %w", ErrDuplicatePendingTask),
			want: true,
		},
		{
			name: "a different unique violation must not trigger a reclaim",
			err:  &pgconn.PgError{Code: "23505", ConstraintName: "agent_workspace_name_unique"},
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("connection reset"),
			want: false,
		},
		{name: "nil", err: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pendingSlotTakenErr(tc.err); got != tc.want {
				t.Fatalf("pendingSlotTakenErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
