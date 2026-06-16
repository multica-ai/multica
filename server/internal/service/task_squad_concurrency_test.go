package service

import (
	"context"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// squadTestDBTX routes QueryRow and Exec calls for maybeReleaseSquadTask tests.
// Each QueryRow call is identified by a query tag embedded in the SQL string
// (the first word after the sqlc name comment). Exec always succeeds unless
// releaseErr is set.
type squadTestDBTX struct {
	// QueryRow responses, keyed by sqlc query tag.
	issue      *db.Issue
	issueErr   error
	squad      *db.Squad
	squadErr   error
	runningCount int64
	runningErr   error
	releaseRows  int64
	releaseErr   error
}

func (m *squadTestDBTX) Exec(_ context.Context, sql string, _ ...interface{}) (pgconn.CommandTag, error) {
	if m.releaseErr != nil {
		return pgconn.NewCommandTag(""), m.releaseErr
	}
	return pgconn.NewCommandTag("UPDATE " + strconv.FormatInt(m.releaseRows, 10)), nil
}

func (m *squadTestDBTX) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	return nil, nil
}

func (m *squadTestDBTX) QueryRow(_ context.Context, sql string, args ...interface{}) pgx.Row {
	// GetIssue: SELECT * FROM issue WHERE id = $1
	if m.issue != nil || m.issueErr != nil {
		return &issueRow{issue: m.issue, err: m.issueErr}
	}
	// GetSquad: SELECT * FROM squad WHERE id = $1
	if m.squad != nil || m.squadErr != nil {
		return &squadRow{squad: m.squad, err: m.squadErr}
	}
	// CountRunningSquadTasks / CountTodayAgentTasks
	return &countRow{count: m.runningCount, err: m.runningErr}
}

// issueRow implements pgx.Row for GetIssue.
type issueRow struct {
	issue *db.Issue
	err   error
}

func (r *issueRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for _, d := range dest {
		switch v := d.(type) {
		case *pgtype.UUID:
			*v = r.issue.ID
		case *string:
			*v = r.issue.AssigneeType
		case *pgtype.Text:
			if v == &r.issue.AssigneeType {
				// already handled above as string
			}
		}
	}
	return nil
}

// squadRow implements pgx.Row for GetSquad.
type squadRow struct {
	squad *db.Squad
	err   error
}

func (r *squadRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for _, d := range dest {
		switch v := d.(type) {
		case *pgtype.UUID:
			*v = r.squad.ID
		case *string:
			*v = r.squad.Name
		case *int32:
			*v = r.squad.MaxConcurrentTasks
		}
	}
	return nil
}

// countRow implements pgx.Row for count queries (returns int64).
type countRow struct {
	count int64
	err   error
}

func (r *countRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for _, d := range dest {
		if v, ok := d.(*int64); ok {
			*v = r.count
		}
	}
	return nil
}

func TestMaybeReleaseSquadTask(t *testing.T) {
	t.Parallel()

	squadID := testUUID(1)
	issueID := testUUID(2)
	taskID := testUUID(3)

	noLimitSquad := &db.Squad{ID: squadID, MaxConcurrentTasks: 0}
	capOneSquad := &db.Squad{ID: squadID, MaxConcurrentTasks: 1}
	squadIssue := &db.Issue{ID: issueID, AssigneeType: "squad"}
	agentIssue := &db.Issue{ID: issueID, AssigneeType: "agent"}

	task := db.AgentTaskQueue{ID: taskID, IssueID: issueID}
	taskNoIssue := db.AgentTaskQueue{ID: taskID, IssueID: pgtype.UUID{}}

	tests := []struct {
		name    string
		mock    squadTestDBTX
		task    db.AgentTaskQueue
		want    bool
		wantReason string
	}{
		{
			name: "no issue id → false",
			mock: squadTestDBTX{},
			task: taskNoIssue,
			want: false,
		},
		{
			name: "non-squad issue → false",
			mock: squadTestDBTX{issue: agentIssue},
			task: task,
			want: false,
		},
		{
			name: "max_concurrent_tasks = 0 → false",
			mock: squadTestDBTX{issue: squadIssue, squad: noLimitSquad},
			task: task,
			want: false,
		},
		{
			name: "under limit (1 running, max=1) → true released",
			mock: squadTestDBTX{
				issue:        squadIssue,
				squad:        capOneSquad,
				runningCount: 1,
				releaseRows:  1,
			},
			task:       task,
			want:       true,
			wantReason: "squad_max_concurrent",
		},
		{
			name: "under limit (0 running, max=1) → false",
			mock: squadTestDBTX{
				issue:        squadIssue,
				squad:        capOneSquad,
				runningCount: 0,
			},
			task: task,
			want: false,
		},
		{
			name: "release returns 0 rows → false (race with another goroutine)",
			mock: squadTestDBTX{
				issue:        squadIssue,
				squad:        capOneSquad,
				runningCount: 2,
				releaseRows:  0, // another goroutine already touched the task
			},
			task:       task,
			want:       false,
		},
		{
			name: "release DB error → false (fail open)",
			mock: squadTestDBTX{
				issue:        squadIssue,
				squad:        capOneSquad,
				runningCount: 1,
				releaseErr:   pgx.ErrNoRows,
			},
			task: task,
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := tc.mock // copy
			q := db.New(&mock)
			svc := &TaskService{Queries: q}

			released, reason := svc.maybeReleaseSquadTask(context.Background(), tc.task)
			if released != tc.want {
				t.Errorf("released = %v, want %v", released, tc.want)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}
