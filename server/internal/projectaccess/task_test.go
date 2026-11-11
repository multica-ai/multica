package projectaccess

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type taskBatchQuerier struct {
	issueCalls, runCalls int
	issueIDs, runIDs     []pgtype.UUID
	failIssue, failRun   bool
}

func (q *taskBatchQuerier) ListIssueProjectIDs(_ context.Context, ids []pgtype.UUID) ([]db.ListIssueProjectIDsRow, error) {
	q.issueCalls++
	q.issueIDs = ids
	if q.failIssue {
		return nil, errors.New("db down")
	}
	return []db.ListIssueProjectIDsRow{{ID: uid(10), ProjectID: p1}, {ID: uid(11), ProjectID: p2}}, nil
}

func (q *taskBatchQuerier) ListAutopilotRunProjectIDs(_ context.Context, ids []pgtype.UUID) ([]db.ListAutopilotRunProjectIDsRow, error) {
	q.runCalls++
	q.runIDs = ids
	if q.failRun {
		return nil, errors.New("db down")
	}
	rows := make([]db.ListAutopilotRunProjectIDsRow, len(ids))
	for i, id := range ids {
		rows[i] = db.ListAutopilotRunProjectIDsRow{ID: id, ProjectID: p1}
	}
	return rows, nil
}

func TestTasksProjectsBatchesHistoryAndQuickCreateParents(t *testing.T) {
	var tasks []db.AgentTaskQueue
	for i := range 200 {
		tasks = append(tasks, db.AgentTaskQueue{AutopilotRunID: uid(byte(i + 1))})
	}
	tasks = append(tasks,
		db.AgentTaskQueue{IssueID: uid(10)},
		db.AgentTaskQueue{IssueID: uid(10), AutopilotRunID: uid(1)},
		db.AgentTaskQueue{Context: []byte(fmt.Sprintf(`{"type":"quick_create","project_id":%q,"parent_issue_id":%q}`, util.UUIDToString(p1), util.UUIDToString(uid(11))))},
		db.AgentTaskQueue{ChatSessionID: uid(12), AutopilotRunID: uid(1)},
		db.AgentTaskQueue{Context: []byte(`{"type":"other"}`)},
		db.AgentTaskQueue{Context: []byte(`not json`)},
		db.AgentTaskQueue{Context: []byte(`{"type":"quick_create","project_id":"bad","parent_issue_id":"bad"}`)},
		db.AgentTaskQueue{IssueID: uid(99)},
	)
	q := &taskBatchQuerier{}
	projects, err := TasksProjects(context.Background(), q, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if q.issueCalls != 1 || q.runCalls != 1 || len(q.runIDs) != 200 || len(q.issueIDs) != 3 {
		t.Fatalf("queries: issue=%d (%d IDs), autopilot=%d (%d IDs); want one each with unique references", q.issueCalls, len(q.issueIDs), q.runCalls, len(q.runIDs))
	}
	for i := range 202 {
		if !slices.Equal(projects[i], []pgtype.UUID{p1}) {
			t.Fatalf("run %d projects = %v", i, projects[i])
		}
	}
	if !slices.Equal(projects[202], []pgtype.UUID{p1, p2}) {
		t.Fatalf("cross-project quick-create = %v", projects[202])
	}
	for i := 203; i < len(tasks); i++ {
		if len(projects[i]) != 0 {
			t.Fatalf("run %d should have no projects: %v", i, projects[i])
		}
	}
	hidden := newHidden()
	hidden.add([]pgtype.UUID{p2})
	if hidden.CanSeeProjects(projects[202]) {
		t.Fatal("membership in target alone must not expose a hidden parent")
	}
}

func TestTasksProjectsSkipsUnusedQueriesAndPropagatesErrors(t *testing.T) {
	q := &taskBatchQuerier{}
	if _, err := TasksProjects(context.Background(), q, []db.AgentTaskQueue{{ChatSessionID: uid(1)}, {}}); err != nil {
		t.Fatal(err)
	}
	if q.issueCalls != 0 || q.runCalls != 0 {
		t.Fatal("project-free tasks must not query")
	}
	for _, q := range []*taskBatchQuerier{{failIssue: true}, {failRun: true}} {
		if _, err := TasksProjects(context.Background(), q, []db.AgentTaskQueue{{IssueID: uid(10)}, {AutopilotRunID: uid(1)}}); err == nil {
			t.Fatal("lookup error must not grant access")
		}
	}
}
