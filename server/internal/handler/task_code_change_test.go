package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func codeChangeFixture(t *testing.T, label ...string) (issueID, taskID string) {
	t.Helper()
	name := "code-change " + strings.Join(label, " ")
	runtimeID := dbfx.Runtime(t, name+" runtime")
	agentID := dbfx.Agent(t, name+" agent", runtimeID)
	issueID = dbfx.Issue(t, name+" issue")
	taskID = dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running"})
	dbfx.Cleanup(t, `DELETE FROM task_code_change WHERE task_id = $1`, taskID)
	return issueID, taskID
}

func sampleCodeChangeRepo(patch string) map[string]any {
	return map[string]any{
		"path": "app", "branch": "agent/fix-login", "base": "1111111", "head": "2222222",
		"files": []any{
			map[string]any{"path": "main.go", "status": "modified", "additions": 2, "deletions": 1},
			map[string]any{"path": "notes.txt", "status": "copied", "additions": 1},
		},
		"patch": patch,
	}
}

func reportCodeChanges(t *testing.T, taskID string, body any) *testutil.Response {
	t.Helper()
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/code-changes", body), "taskId", taskID)
	return testutil.Call(t, testHandler.ReportTaskCodeChanges, req)
}

func getCodeChanges(t *testing.T, taskID, userID string) *testutil.Response {
	t.Helper()
	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/tasks/"+taskID+"/code-changes", nil), userID), "taskId", taskID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	return testutil.Call(t, testHandler.GetTaskCodeChanges, req)
}

func codeChangeRows(t *testing.T, taskID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM task_code_change WHERE task_id = $1`, taskID)
}

func TestTaskCodeChanges_ReportThenReadAndReplace(t *testing.T) {
	issueID, taskID := codeChangeFixture(t)

	// Before the daemon reports, the reader gets no repos and a null time.
	var empty TaskCodeChangesResponse
	getCodeChanges(t, taskID, testUserID).Want(http.StatusOK).JSON(&empty)
	if empty.TaskID != taskID || len(empty.Repos) != 0 || empty.UpdatedAt != nil || empty.ReposTruncated {
		t.Fatalf("before report = %+v", empty)
	}

	token := "ghp_" + strings.Repeat("A", 36)
	patch := "diff --git a/main.go b/main.go\n+bin\x00ary\n+const key = \"" + token + "\"\n"
	reportCodeChanges(t, taskID, map[string]any{"repos": []any{sampleCodeChangeRepo(patch)}}).Want(http.StatusNoContent)

	var got TaskCodeChangesResponse
	getCodeChanges(t, taskID, testUserID).Want(http.StatusOK).JSON(&got)
	if len(got.Repos) != 1 || got.UpdatedAt == nil {
		t.Fatalf("after report = %+v", got)
	}
	repo := got.Repos[0]
	if repo.Path != "app" || repo.Branch != "agent/fix-login" || repo.Base != "1111111" || len(repo.Files) != 2 {
		t.Fatalf("repo = %+v", repo)
	}
	if f := repo.Files[0]; f.Path != "main.go" || f.Additions != 2 || f.Deletions != 1 {
		t.Fatalf("file = %+v", f)
	}
	if repo.Files[1].Status != "modified" {
		t.Fatalf("unknown status = %q, want it stored as modified", repo.Files[1].Status)
	}
	if strings.Contains(repo.Patch, "\x00") || !strings.Contains(repo.Patch, "+binary") {
		t.Fatalf("patch not sanitized: %q", repo.Patch)
	}
	if strings.Contains(repo.Patch, token) {
		t.Fatalf("patch kept a credential: %q", repo.Patch)
	}
	var storedIssue, storedWorkspace string
	if err := testPool.QueryRow(context.Background(),
		`SELECT issue_id::text, workspace_id::text FROM task_code_change WHERE task_id = $1`, taskID,
	).Scan(&storedIssue, &storedWorkspace); err != nil {
		t.Fatalf("load stored row: %v", err)
	}
	if storedIssue != issueID || storedWorkspace != testWorkspaceID {
		t.Fatalf("row keys = issue %s workspace %s", storedIssue, storedWorkspace)
	}

	// A later report replaces the snapshot instead of adding to it.
	reportCodeChanges(t, taskID, map[string]any{"repos": []any{}}).Want(http.StatusNoContent)
	getCodeChanges(t, taskID, testUserID).Want(http.StatusOK).JSON(&got)
	if len(got.Repos) != 0 || got.UpdatedAt == nil {
		t.Fatalf("after empty report = %+v, want replaced", got)
	}
	if n := codeChangeRows(t, taskID); n != 1 {
		t.Fatalf("rows after re-report = %d, want 1", n)
	}
}

func TestTaskCodeChanges_RejectsOversizedAndMalformedBodies(t *testing.T) {
	_, taskID := codeChangeFixture(t)
	big := map[string]any{"repos": []any{map[string]any{
		"path": ".", "base": "abc", "files": []any{}, "patch": strings.Repeat("x", maxTaskCodeChangeBodyBytes),
	}}}
	reportCodeChanges(t, taskID, big).Want(http.StatusRequestEntityTooLarge)
	reportCodeChanges(t, taskID, map[string]any{"repos": "not a list"}).Want(http.StatusBadRequest)
	if n := codeChangeRows(t, taskID); n != 0 {
		t.Fatalf("rejected reports stored %d rows", n)
	}
}

func TestTaskCodeChanges_CapsReposAndFiles(t *testing.T) {
	_, taskID := codeChangeFixture(t)
	files := make([]any, maxTaskCodeChangeFiles+5)
	for i := range files {
		files[i] = map[string]any{"path": "f", "status": "added", "additions": 1}
	}
	repos := []any{map[string]any{"path": "big", "base": "abc", "files": files, "patch": ""}}
	for len(repos) < maxTaskCodeChangeRepos+3 {
		repos = append(repos, map[string]any{"path": "repo", "base": "abc", "files": []any{}, "patch": ""})
	}
	reportCodeChanges(t, taskID, map[string]any{"repos": repos}).Want(http.StatusNoContent)

	var got TaskCodeChangesResponse
	getCodeChanges(t, taskID, testUserID).Want(http.StatusOK).JSON(&got)
	if len(got.Repos) != maxTaskCodeChangeRepos || !got.ReposTruncated {
		t.Fatalf("repos = %d truncated=%v, want %d and truncated", len(got.Repos), got.ReposTruncated, maxTaskCodeChangeRepos)
	}
	if first := got.Repos[0]; len(first.Files) != maxTaskCodeChangeFiles || !first.FilesTruncated {
		t.Fatalf("files = %d truncated=%v", len(first.Files), first.FilesTruncated)
	}

	// The daemon's own flag is stored, and a complete report clears it.
	reportCodeChanges(t, taskID, map[string]any{"repos": []any{}, "repos_truncated": true}).Want(http.StatusNoContent)
	getCodeChanges(t, taskID, testUserID).Want(http.StatusOK).JSON(&got)
	if !got.ReposTruncated {
		t.Fatal("daemon repos_truncated not stored")
	}
	reportCodeChanges(t, taskID, map[string]any{"repos": []any{}}).Want(http.StatusNoContent)
	getCodeChanges(t, taskID, testUserID).Want(http.StatusOK).JSON(&got)
	if got.ReposTruncated {
		t.Fatal("a complete report must clear repos_truncated")
	}
}

func TestTaskCodeChanges_OtherWorkspaceDaemonCannotReport(t *testing.T) {
	_, taskID := codeChangeFixture(t)
	req := withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/code-changes",
		map[string]any{"repos": []any{sampleCodeChangeRepo("")}},
		"00000000-0000-0000-0000-000000000000", "attacker-daemon"), "taskId", taskID)
	testutil.Call(t, testHandler.ReportTaskCodeChanges, req).Want(http.StatusNotFound)
	if n := codeChangeRows(t, taskID); n != 0 {
		t.Fatalf("foreign daemon stored %d rows", n)
	}
}

// Code changes are readable exactly where the run's transcript is.
func TestTaskCodeChanges_FollowsRunVisibility(t *testing.T) {
	t.Run("private project", func(t *testing.T) {
		f := newPrivateProjectFixture(t)
		agentID := seededHandlerTestAgent(t)
		taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": f.secretIssue, "runtime_id": testRuntimeID})
		dbfx.Cleanup(t, `DELETE FROM task_code_change WHERE task_id = $1`, taskID)
		reportCodeChanges(t, taskID, map[string]any{"repos": []any{sampleCodeChangeRepo("")}}).Want(http.StatusNoContent)

		getCodeChanges(t, taskID, f.outsider).Want(http.StatusNotFound)
		getCodeChanges(t, taskID, f.outsideAdmin).Want(http.StatusNotFound)
		var got TaskCodeChangesResponse
		getCodeChanges(t, taskID, f.insider).Want(http.StatusOK).JSON(&got)
		if len(got.Repos) != 1 {
			t.Fatalf("insider read = %+v", got)
		}
	})

	t.Run("chat session", func(t *testing.T) {
		f := seedChatTaskPrivacy(t)
		dbfx.Cleanup(t, `DELETE FROM task_code_change WHERE task_id = $1`, f.chatTaskID)
		reportCodeChanges(t, f.chatTaskID, map[string]any{"repos": []any{sampleCodeChangeRepo("")}}).Want(http.StatusNoContent)

		getCodeChanges(t, f.chatTaskID, testUserID).Want(http.StatusNotFound)
		getCodeChanges(t, f.chatTaskID, f.ownerID).Want(http.StatusOK)
		getCodeChanges(t, f.issueTaskID, testUserID).Want(http.StatusOK)
	})
}

func TestTaskCodeChanges_RemovedWithIssueTaskBatchAndWorkspace(t *testing.T) {
	ctx := context.Background()
	issueID, taskID := codeChangeFixture(t)
	reportCodeChanges(t, taskID, map[string]any{"repos": []any{sampleCodeChangeRepo("")}}).Want(http.StatusNoContent)
	if err := testHandler.Queries.DeleteIssue(ctx, db.DeleteIssueParams{
		ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	if n := codeChangeRows(t, taskID); n != 0 {
		t.Fatalf("code changes survived issue deletion (%d rows)", n)
	}

	_, batchTask := codeChangeFixture(t, "batch")
	reportCodeChanges(t, batchTask, map[string]any{"repos": []any{sampleCodeChangeRepo("")}}).Want(http.StatusNoContent)
	if err := testHandler.Queries.DeleteTaskBatch(ctx, []pgtype.UUID{parseUUID(batchTask)}); err != nil {
		t.Fatalf("delete task batch: %v", err)
	}
	if n := codeChangeRows(t, batchTask); n != 0 {
		t.Fatalf("code changes survived task batch deletion (%d rows)", n)
	}

	// The workspace leaf sweep removes rows by workspace_id even when their
	// task is already gone. A workspace id no other row uses keeps the sweep
	// away from the shared fixture workspace.
	orphanWorkspace := "00000000-0000-4000-8000-00000000c0de"
	orphanTask := "00000000-0000-4000-8000-00000000c0df"
	dbfx.Cleanup(t, `DELETE FROM task_code_change WHERE task_id = $1`, orphanTask)
	if _, err := testPool.Exec(ctx,
		`INSERT INTO task_code_change (task_id, workspace_id) VALUES ($1, $2)`, orphanTask, orphanWorkspace); err != nil {
		t.Fatalf("insert orphan row: %v", err)
	}
	if err := testHandler.Queries.DeleteWorkspaceLeafData(ctx, parseUUID(orphanWorkspace)); err != nil {
		t.Fatalf("delete workspace leaf data: %v", err)
	}
	if n := codeChangeRows(t, orphanTask); n != 0 {
		t.Fatalf("code changes survived workspace teardown (%d rows)", n)
	}
}

// The execution log carries each finished run's changed-file count, so the
// UI can hide the code-changes entry on runs that changed nothing.
func TestTaskCodeChanges_FileCountOnExecutionLog(t *testing.T) {
	issueID, changedID := codeChangeFixture(t, "count")
	var agentID, runtimeID string
	dbfx.QueryRow(t, `SELECT agent_id::text, runtime_id::text FROM agent_task_queue WHERE id = $1`, changedID).Scan(&agentID, &runtimeID)
	emptyID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running"})
	noReportID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "completed", "completed_at": testutil.Raw("now()")})
	runningID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running"})
	dbfx.Cleanup(t, `DELETE FROM task_code_change WHERE task_id = ANY($1::uuid[])`, []string{emptyID, runningID})

	second := sampleCodeChangeRepo("")
	second["path"] = "lib"
	second["files"] = []any{map[string]any{"path": "util.go", "status": "added", "additions": 4}}
	reportCodeChanges(t, changedID, map[string]any{"repos": []any{sampleCodeChangeRepo(""), second}}).Want(http.StatusNoContent)
	reportCodeChanges(t, emptyID, map[string]any{"repos": []any{}}).Want(http.StatusNoContent)
	reportCodeChanges(t, runningID, map[string]any{"repos": []any{sampleCodeChangeRepo("")}}).Want(http.StatusNoContent)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = ANY($1::uuid[])`, []string{changedID, emptyID})

	var runs []AgentTaskResponse
	testutil.Call(t, testHandler.ListTasksByIssue, withURLParam(newRequest(http.MethodGet, "/api/issues/"+issueID+"/tasks", nil), "id", issueID)).
		Want(http.StatusOK).JSON(&runs)
	got := map[string]int{}
	for _, run := range runs {
		got[run.ID] = run.CodeChangeFiles
	}
	want := map[string]int{changedID: 3, emptyID: 0, noReportID: 0, runningID: 0}
	for id, n := range want {
		if c, ok := got[id]; !ok || c != n {
			t.Errorf("task %s: code_change_files = %d (listed %v), want %d", id, c, ok, n)
		}
	}
}

// A run's code changes come from what the agent's tools did, so an agent that
// restricts its tool details restricts them too: only the agent owner and the
// workspace's owners and admins read them, and the execution log shows the
// entry only to them.
func TestTaskCodeChanges_FollowToolDetailsVisibility(t *testing.T) {
	issueID, taskID := codeChangeFixture(t, "tool details")
	reportCodeChanges(t, taskID, map[string]any{"repos": []any{sampleCodeChangeRepo("diff --git a/main.go b/main.go\n")}}).Want(http.StatusNoContent)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskID)
	agentOwner := createTestUserAndMember(t, "member")
	dbfx.Exec(t, `UPDATE agent SET owner_id = $1, tool_details_visibility = 'restricted'
		WHERE id = (SELECT agent_id FROM agent_task_queue WHERE id = $2)`, agentOwner, taskID)
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE owner_id = $2`, testUserID, agentOwner)
	})
	admin := createTestUserAndMember(t, "admin")
	member := createTestUserAndMember(t, "member")
	asAgent := map[string]string{"X-Actor-Source": "task_token", "X-Agent-ID": "00000000-0000-0000-0000-000000000001"}

	read := func(t *testing.T, userID string, headers map[string]string) TaskCodeChangesResponse {
		t.Helper()
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/tasks/"+taskID+"/code-changes", nil), userID), "taskId", taskID)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
		var got TaskCodeChangesResponse
		testutil.Call(t, testHandler.GetTaskCodeChanges, req).Want(http.StatusOK).JSON(&got)
		return got
	}
	fileCount := func(t *testing.T, userID string, headers map[string]string) int {
		t.Helper()
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+issueID+"/tasks", nil), userID), "id", issueID)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		var runs []AgentTaskResponse
		testutil.Call(t, testHandler.ListTasksByIssue, req).Want(http.StatusOK).JSON(&runs)
		for _, run := range runs {
			if run.ID == taskID {
				return run.CodeChangeFiles
			}
		}
		t.Fatalf("run %s not in the execution log", taskID)
		return 0
	}

	for _, tc := range []struct {
		name    string
		userID  string
		headers map[string]string
		full    bool
	}{
		{name: "agent owner", userID: agentOwner, full: true},
		{name: "workspace owner", userID: testUserID, full: true},
		{name: "workspace admin", userID: admin, full: true},
		{name: "plain member", userID: member},
		{name: "an agent acting for the workspace owner", userID: testUserID, headers: asAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := read(t, tc.userID, tc.headers)
			n := fileCount(t, tc.userID, tc.headers)
			if tc.full {
				if got.DetailsHidden || len(got.Repos) != 1 || n != 2 {
					t.Errorf("reader got hidden=%v repos=%d files=%d, want the changes and 2 files", got.DetailsHidden, len(got.Repos), n)
				}
			} else if !got.DetailsHidden || len(got.Repos) != 0 || got.UpdatedAt != nil || n != 0 {
				t.Errorf("non-reader got hidden=%v repos=%d updated=%v files=%d, want nothing", got.DetailsHidden, len(got.Repos), got.UpdatedAt, n)
			}
		})
	}

	dbfx.Exec(t, `UPDATE agent SET tool_details_visibility = 'everyone'
		WHERE id = (SELECT agent_id FROM agent_task_queue WHERE id = $1)`, taskID)
	if got, n := read(t, member, nil), fileCount(t, member, nil); got.DetailsHidden || len(got.Repos) != 1 || n != 2 {
		t.Errorf("an unrestricted agent hid its code changes from a member: hidden=%v repos=%d files=%d", got.DetailsHidden, len(got.Repos), n)
	}
}
