package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The claim path resolves project context from a SOFT reference: issue.project_id,
// chat_session.project_id and the quick-create context JSONB are all plain
// columns with no foreign key behind them (see the repo's no-FK rule). These
// tests pin the tenant boundary that resolveClaimProjectContext is responsible
// for, using the only states that can actually produce a cross-tenant read:
// a dangling reference to a project in another workspace, and a project_resource
// row whose own workspace_id disagrees with its project's.
//
// Both were reachable before MUL-6547: the issue and quick-create branches read
// the project with GetProject (no workspace predicate) and every branch listed
// resources by project_id alone.

// foreignWorkspaceWithProject builds a complete project in a DIFFERENT workspace
// and returns (workspaceID, projectID). The caller points a same-workspace task
// at projectID to simulate the corrupt reference.
func foreignWorkspaceWithProject(t *testing.T, slug, prefix string) (string, string) {
	t.Helper()

	foreignWorkspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Foreign " + slug,
		"slug":         slug,
		"description":  "",
		"issue_prefix": prefix,
	})
	foreignProjectID := dbfx.Project(t, foreignProjectTitle, testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"description":  foreignProjectDescription,
	})
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    foreignProjectID,
		"workspace_id":  foreignWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  `{"url":"` + foreignRepoURL + `"}`,
		"position":      0,
	})
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    foreignProjectID,
		"workspace_id":  foreignWorkspaceID,
		"resource_type": "local_directory",
		"resource_ref":  `{"daemon_id":"foreign-daemon","local_path":"` + foreignLocalPath + `","execution_mode":"in_place"}`,
		"position":      1,
	})
	return foreignWorkspaceID, foreignProjectID
}

const (
	foreignProjectTitle       = "Foreign project must not leak"
	foreignProjectDescription = "Foreign project instructions must not leak"
	foreignRepoURL            = "https://github.com/example/foreign-tenant-repo"
	foreignLocalPath          = "/srv/foreign-tenant-project"
	localFallbackRepoURL      = "https://github.com/example/local-workspace-fallback"
)

// assertNoForeignContext fails when any part of the foreign tenant's project
// reached the claim response, on any field the daemon forwards to the agent.
func assertNoForeignContext(t *testing.T, body string, extra ...string) {
	t.Helper()
	for _, leaked := range append([]string{
		foreignProjectTitle,
		foreignProjectDescription,
		foreignRepoURL,
		foreignLocalPath,
	}, extra...) {
		if leaked == "" {
			continue
		}
		if strings.Contains(body, leaked) {
			t.Errorf("claim response leaked foreign tenant context %q: %s", leaked, body)
		}
	}
}

type claimProjectFields struct {
	WorkspaceID        string                `json:"workspace_id"`
	Repos              []RepoData            `json:"repos"`
	ProjectID          string                `json:"project_id"`
	ProjectTitle       string                `json:"project_title"`
	ProjectDescription string                `json:"project_description"`
	ProjectResources   []ProjectResourceData `json:"project_resources"`
}

// An issue whose project_id points at another workspace's project must degrade
// to workspace context. Before MUL-6547 the issue branch used GetProject, which
// has no workspace predicate, so the foreign project's title, description,
// repository URL and local path were all serialized into the claim.
func TestClaimTask_IssueProjectInForeignWorkspace_DegradesToWorkspaceRepos(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	setHandlerTestWorkspaceRepos(t, []map[string]string{
		{"url": localFallbackRepoURL, "description": "local"},
	})
	_, foreignProjectID := foreignWorkspaceWithProject(t, "foreign-issue-project-ws", "FIP")

	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	issueID := dbfx.Issue(t, "issue pointing at a foreign project", testutil.Cols{
		"project_id": foreignProjectID,
		"priority":   "medium",
		"number":     88101,
	})
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, "test-claim-foreign-issue-project")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *claimProjectFields `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatal("expected task in response")
	}
	assertNoForeignContext(t, w.Text(), foreignProjectID)

	if resp.Task.ProjectID != "" {
		t.Errorf("project_id = %q, want empty for an out-of-workspace project reference", resp.Task.ProjectID)
	}
	if len(resp.Task.ProjectResources) != 0 {
		t.Errorf("project_resources = %+v, want none", resp.Task.ProjectResources)
	}
	if len(resp.Task.Repos) != 1 || resp.Task.Repos[0].URL != localFallbackRepoURL {
		t.Fatalf("repos = %+v, want only the local workspace fallback", resp.Task.Repos)
	}
}

// The quick-create branch resolves its project from the task context JSONB
// rather than an issue row, and used the same unscoped GetProject. It needs its
// own assertion: a requester who edits the stored project_id must not be able to
// pull another tenant's resources into the claim.
func TestClaimTask_QuickCreateProjectInForeignWorkspace_DegradesToWorkspaceRepos(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	setHandlerTestWorkspaceRepos(t, []map[string]string{
		{"url": localFallbackRepoURL, "description": "local"},
	})
	_, foreignProjectID := foreignWorkspaceWithProject(t, "foreign-qc-project-ws", "FQP")

	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	quickContext, _ := json.Marshal(map[string]any{
		"type":         "quick_create",
		"prompt":       "create a follow-up issue",
		"requester_id": testUserID,
		"workspace_id": testWorkspaceID,
		"project_id":   foreignProjectID,
	})
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context)
		VALUES ($1, $2, 'queued', 2, $3)
	`, agentID, runtimeID, quickContext)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *claimProjectFields `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatal("expected task in response")
	}
	assertNoForeignContext(t, w.Text(), foreignProjectID)

	if resp.Task.ProjectID != "" {
		t.Errorf("quick-create project_id = %q, want empty for an out-of-workspace project", resp.Task.ProjectID)
	}
	if len(resp.Task.ProjectResources) != 0 {
		t.Errorf("quick-create project_resources = %+v, want none", resp.Task.ProjectResources)
	}
	if len(resp.Task.Repos) != 1 || resp.Task.Repos[0].URL != localFallbackRepoURL {
		t.Fatalf("quick-create repos = %+v, want only the local workspace fallback", resp.Task.Repos)
	}
}

// A project_resource row carries its own workspace_id, so a resource can
// disagree with the project it hangs off. Listing by project_id alone — which
// every claim branch did before MUL-6547 — serialized that row anyway. The
// in-workspace resource on the same project must still come through, proving the
// new predicate filters rather than just emptying the list.
func TestClaimTask_ProjectResourceFromForeignWorkspace_IsFilteredOut(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	foreignWorkspaceID, _ := foreignWorkspaceWithProject(t, "foreign-resource-ws", "FRW")

	const localRepoURL = "https://github.com/example/local-project-repo"
	projectID := dbfx.Project(t, "Project with a mismatched resource row")
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  testWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  `{"url":"` + localRepoURL + `"}`,
		"position":      0,
	})
	// Same project, foreign workspace_id: only reachable through corrupt data,
	// which is exactly the case the predicate exists for.
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  foreignWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  `{"url":"` + foreignRepoURL + `"}`,
		"position":      1,
	})
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  foreignWorkspaceID,
		"resource_type": "local_directory",
		"resource_ref":  `{"daemon_id":"foreign-daemon","local_path":"` + foreignLocalPath + `","execution_mode":"in_place"}`,
		"position":      2,
	})

	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	issueID := dbfx.Issue(t, "issue with a mismatched project resource", testutil.Cols{
		"project_id": projectID,
		"priority":   "medium",
		"number":     88102,
	})
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, "test-claim-foreign-resource")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *claimProjectFields `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatal("expected task in response")
	}
	assertNoForeignContext(t, w.Text())

	if resp.Task.ProjectID != projectID {
		t.Errorf("project_id = %q, want %q", resp.Task.ProjectID, projectID)
	}
	if len(resp.Task.ProjectResources) != 1 {
		t.Fatalf("project_resources count = %d, want only the in-workspace row: %+v",
			len(resp.Task.ProjectResources), resp.Task.ProjectResources)
	}
	if len(resp.Task.Repos) != 1 || resp.Task.Repos[0].URL != localRepoURL {
		t.Fatalf("repos = %+v, want only the in-workspace project repo", resp.Task.Repos)
	}
}

// A run_only autopilot owns its task's workspace. taskToResponse seeds
// resp.WorkspaceID with the RUNTIME's workspace, and the branch used to widen it
// only `if resp.WorkspaceID == ""` — a condition that never held, so the
// isolation check compared the runtime workspace against itself and a
// cross-workspace autopilot_run_id was dispatched instead of rejected.
func TestClaimTask_AutopilotRunOnlyForeignWorkspace_CancelsTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var localAgentID, localRuntimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&localAgentID, &localRuntimeID)

	foreignWorkspaceID, foreignProjectID := foreignWorkspaceWithProject(t, "foreign-autopilot-ws", "FAW")

	const foreignAutopilotTitle = "Foreign autopilot must not leak"
	foreignAutopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    foreignWorkspaceID,
		"project_id":      foreignProjectID,
		"title":           foreignAutopilotTitle,
		"assignee_id":     localAgentID,
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	foreignRunID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": foreignAutopilotID,
		"source":       "manual",
		"status":       "running",
	})
	taskID := dbfx.Task(t, localAgentID, testutil.Cols{
		"runtime_id":       localRuntimeID,
		"issue_id":         nil,
		"autopilot_run_id": foreignRunID,
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+localRuntimeID+"/claim", nil,
		testWorkspaceID, "test-claim-foreign-autopilot")
	req = withURLParam(req, "runtimeId", localRuntimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusInternalServerError)
	if !strings.Contains(w.Text(), "task workspace isolation check failed") {
		t.Fatalf("claim error = %q, want the workspace isolation failure", w.Text())
	}
	assertNoForeignContext(t, w.Text(), foreignProjectID)
	if strings.Contains(w.Text(), foreignAutopilotTitle) {
		t.Errorf("claim error leaked the foreign autopilot title: %s", w.Text())
	}

	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("cross-workspace autopilot task status = %q, want cancelled", status)
	}
}

// failQueryDBTX delegates every query to the real pool except the ones whose
// SQL contains failOn, which fail with a transient-looking error. db.Queries is
// built on the DBTX interface, so this is the seam that lets a single source
// read fail while the rest of the claim proceeds normally — the shape of a real
// DB blip, which a globally broken pool cannot reproduce (it trips the earlier
// agent-load guard instead).
type failQueryDBTX struct {
	db.DBTX
	failOn string
	err    error
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

func (f failQueryDBTX) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, f.failOn) {
		return errRow{err: f.err}
	}
	return f.DBTX.QueryRow(ctx, sql, args...)
}

func (f failQueryDBTX) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, f.failOn) {
		return nil, f.err
	}
	return f.DBTX.Query(ctx, sql, args...)
}

// A claim resolves its SOURCE row — issue, chat session, autopilot_run,
// autopilot — before anything else. Those reads used to be written as
// `if x, err := ...; err == nil {`, with no else: a failed read silently
// skipped the whole branch. taskToResponse has already seeded resp.WorkspaceID
// with the RUNTIME's workspace by then, so the skipped claim sailed through the
// builder's backstop isolation check (which compared that seed against itself)
// and dispatched an agent with no issue, no chat input, or no autopilot
// instructions at all.
//
// A transient read failure must instead produce NO claim payload and preserve
// the task for the stale-dispatched reclaim — cancelling would destroy a valid
// task over a momentary DB error.
func TestBuildClaimedTaskResponse_SourceLoadFailure_PreservesTaskAndEmitsNoContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	projectID := dbfx.Project(t, "Source load failure project")
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  testWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  `{"url":"https://github.com/example/must-not-appear"}`,
		"position":      0,
	})
	issueID := dbfx.Issue(t, "source load failure issue", testutil.Cols{
		"project_id": projectID,
		"priority":   "medium",
		"number":     88201,
	})
	chatSessionID := dbfx.ChatSession(t, agentID, testutil.Cols{"project_id": projectID})

	for _, tc := range []struct {
		name   string
		failOn string
		cols   testutil.Cols
	}{
		{
			name:   "issue",
			failOn: "FROM issue\nWHERE id = $1",
			cols:   testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID},
		},
		{
			name:   "chat session",
			failOn: "FROM chat_session\nWHERE id = $1",
			cols:   testutil.Cols{"runtime_id": runtimeID, "issue_id": nil, "chat_session_id": chatSessionID},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskID := dbfx.Task(t, agentID, tc.cols)
			dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'dispatched' WHERE id = $1`, taskID)

			task := db.AgentTaskQueue{
				ID:        parseUUID(taskID),
				AgentID:   parseUUID(agentID),
				RuntimeID: parseUUID(runtimeID),
				Status:    "dispatched",
			}
			if issueRef, ok := tc.cols["issue_id"].(string); ok {
				task.IssueID = parseUUID(issueRef)
			}
			if chatRef, ok := tc.cols["chat_session_id"].(string); ok {
				task.ChatSessionID = parseUUID(chatRef)
			}

			brokenHandler := *testHandler
			brokenHandler.Queries = db.New(failQueryDBTX{
				DBTX:   testPool,
				failOn: tc.failOn,
				err:    errors.New("simulated transient read failure"),
			})

			runtime := db.AgentRuntime{
				ID:          parseUUID(runtimeID),
				WorkspaceID: parseUUID(testWorkspaceID),
			}
			req := httptest.NewRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil)

			resp, _, _, _, failure := brokenHandler.buildClaimedTaskResponse(
				req, &task, runtime, runtimeID, testWorkspaceID)

			if failure == nil {
				t.Fatal("expected the claim to be rejected when its source row cannot be read")
			}
			if failure.outcome != "error_source_load" {
				t.Errorf("outcome = %q, want error_source_load", failure.outcome)
			}
			if failure.status != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", failure.status)
			}

			// No context may be assembled from a source the handler could not read.
			if resp.ThreadName != "" {
				t.Errorf("thread_name = %q, want empty on a failed source read", resp.ThreadName)
			}
			if resp.ProjectID != "" || len(resp.ProjectResources) != 0 || len(resp.Repos) != 0 {
				t.Errorf("project context leaked into a failed claim: project_id=%q resources=%d repos=%d",
					resp.ProjectID, len(resp.ProjectResources), len(resp.Repos))
			}

			// A transient failure preserves the task for the reclaim path.
			var status string
			dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
			if status != "dispatched" {
				t.Errorf("task status = %q, want it preserved as dispatched for reclaim", status)
			}
		})
	}
}

// The other half of the split: a source row that is GONE cannot be retried into
// existence, so it settles terminally instead of spinning in the reclaim loop.
// Legacy FKs keep this state from arising today (agent_task_queue.issue_id ON
// DELETE CASCADE and friends), so the branch is exercised directly rather than
// through a claim.
func TestRejectClaimSourceLoad_MissingRow_CancelsTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": nil})
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'dispatched' WHERE id = $1`, taskID)
	task := db.AgentTaskQueue{ID: parseUUID(taskID), AgentID: parseUUID(agentID), Status: "dispatched"}

	failure := testHandler.rejectClaimSourceLoad(
		context.Background(), &task, pgx.ErrNoRows, "issue", uuidToString(task.IssueID))
	if failure == nil {
		t.Fatal("expected a failure for a missing source row")
	}
	if failure.outcome != "error_source_missing" {
		t.Errorf("outcome = %q, want error_source_missing", failure.outcome)
	}
	if failure.message != "task is missing its issue" {
		t.Errorf("message = %q, want it to name the missing source", failure.message)
	}

	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "cancelled" {
		t.Errorf("task status = %q, want cancelled for an unrecoverable source reference", status)
	}
}
