package handler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type moveCommitFailureStarter struct{ txStarter }
type moveCommitFailureTx struct{ pgx.Tx }

func (s moveCommitFailureStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return moveCommitFailureTx{tx}, nil
}

// All SQL executes on PostgreSQL; only the commit is deliberately rejected.
func (tx moveCommitFailureTx) Commit(context.Context) error {
	return errors.New("injected move commit failure")
}

func TestProjectMoveRollback(t *testing.T) {
	if testPool == nil {
		t.Fatal("PostgreSQL required")
	}
	source, target, project := moveFixture(t)
	issue := dbfx.Issue(t, "Rollback", testutil.Cols{"workspace_id": source, "project_id": project, "number": 7})
	comment := dbfx.Comment(t, issue, "Keep", testutil.Cols{"workspace_id": source})
	dbfx.Insert(t, "pinned_item", testutil.Cols{"workspace_id": source, "user_id": testUserID, "item_type": "project", "item_id": project, "position": 0})
	beforeSource, beforeTarget := moveSearchCursor(t, source), moveSearchCursor(t, target)
	plan := previewMove(t, source, target, project)
	h := *testHandler
	h.TxStarter = moveCommitFailureStarter{h.TxStarter}
	testutil.Call(t, h.MoveProject, moveRequest(source, target, project, plan.PlanHash)).Want(500)
	for _, tc := range []struct{ table, id string }{{"project", project}, {"issue", issue}, {"comment", comment}} {
		if n := dbfx.Count(t, "SELECT count(*) FROM "+tc.table+" WHERE id=$1 AND workspace_id=$2", tc.id, source); n != 1 {
			t.Errorf("%s mutation escaped rollback", tc.table)
		}
	}
	if dbfx.Count(t, "SELECT count(*) FROM pinned_item WHERE item_id=$1", project) != 1 || dbfx.Count(t, "SELECT count(*) FROM issue_key_alias WHERE issue_id=$1", issue) != 0 || dbfx.Count(t, "SELECT count(*) FROM project_move WHERE project_id=$1", project) != 0 {
		t.Fatal("cleanup or audit escaped rollback")
	}
	var number, counter int
	dbfx.QueryRow(t, "SELECT number FROM issue WHERE id=$1", issue).Scan(&number)
	dbfx.QueryRow(t, "SELECT issue_counter FROM workspace WHERE id=$1", target).Scan(&counter)
	if number != 7 || counter != 100 {
		t.Fatalf("number=%d counter=%d", number, counter)
	}
	if after := previewMove(t, source, target, project); after.PlanHash != plan.PlanHash {
		t.Fatal("rolled back state changed plan")
	}
	for _, tc := range []struct{ workspace, cursor string }{{source, beforeSource}, {target, beforeTarget}} {
		changes := moveSearchChanges(t, tc.workspace, tc.cursor)
		if len(changes.Issues)+len(changes.Comments)+len(changes.Projects)+len(changes.Deleted.Issues)+len(changes.Deleted.Comments)+len(changes.Deleted.Projects) != 0 {
			t.Fatalf("search mutation escaped rollback: %+v", changes)
		}
	}
	executeMove(t, source, target, project)
}

func TestProjectMoveInvalidHashAndEmptyProject(t *testing.T) {
	source, target, project := moveFixture(t)
	for _, hash := range []string{"", strings.Repeat("z", 64), strings.Repeat("a", 63)} {
		testutil.Call(t, testHandler.MoveProject, moveRequest(source, target, project, hash)).Want(400)
	}
	testutil.Call(t, testHandler.MoveProject, moveRequest(source, target, project, strings.Repeat("a", 64))).Want(409)
	executeMove(t, source, target, project)
	if n := dbfx.Count(t, "SELECT count(*) FROM project WHERE id=$1 AND workspace_id=$2", project, target); n != 1 {
		t.Fatal("empty project not moved")
	}
}

func TestProjectMoveAgentActorDenied(t *testing.T) {
	source, target, project := moveFixture(t)
	runtime := dbfx.Runtime(t, "Move runtime", testutil.Cols{"workspace_id": source})
	agent := dbfx.Agent(t, "Move agent", runtime, testutil.Cols{"workspace_id": source})
	task := dbfx.Task(t, agent, testutil.Cols{"issue_id": nil, "runtime_id": runtime, "status": "completed"})
	req := moveRequest(source, target, project, "")
	req.Header.Set("X-Agent-ID", agent)
	req.Header.Set("X-Task-ID", task)
	testutil.Call(t, testHandler.PreviewProjectMove, req).Want(403)
	req = moveRequest(source, target, project, "")
	req.Header.Set("X-Agent-ID", agent)
	req.Header.Set("X-Task-ID", task)
	testutil.Call(t, testHandler.MoveProject, req).Want(403)
}
