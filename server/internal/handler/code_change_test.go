package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
	"github.com/multica-ai/multica/server/internal/testutil"
)

const samplePatch = "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,2 @@\n-a\n+b\n+c\n"

func codeChangeReportBody(scope string) map[string]any {
	return map[string]any{
		"changes": []map[string]any{{
			"scope":       scope,
			"source":      "local_worktree",
			"repo_key":    "https://github.com/acme/app",
			"repo_label":  "acme/app",
			"repo_url":    "https://x-access-token:ghs_secret@github.com/acme/app.git",
			"branch":      "agent/lambda/han-1",
			"base_commit": "1111111111111111111111111111111111111111",
			"head_commit": "2222222222222222222222222222222222222222",
			"file_count":  1,
			"additions":   2,
			"deletions":   1,
			"files": []map[string]any{
				{"path": "main.go", "status": "modified", "additions": 2, "deletions": 1},
			},
			"patch": samplePatch,
		}},
	}
}

// codeChangeFixture is an issue with one agent run on it.
func codeChangeFixture(t *testing.T) (issueID, taskID string) {
	t.Helper()
	runtimeID := dbfx.Runtime(t, "Code change runtime")
	agentID := dbfx.Agent(t, "Code change agent", runtimeID)
	issueID = dbfx.Issue(t, "Code change issue")
	taskID = dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running"})
	dbfx.Cleanup(t, `DELETE FROM task_code_change WHERE task_id = $1`, taskID)
	return issueID, taskID
}

func reportCodeChanges(t *testing.T, taskID string, body any) *testutil.Response {
	t.Helper()
	req := testutil.WithURLParams(newRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/code-changes", body), "taskId", taskID)
	return testutil.Call(t, testHandler.ReportTaskCodeChanges, req)
}

func TestReportTaskCodeChangesStoresAndReadsBack(t *testing.T) {
	origStorage := testHandler.Storage
	store := &mockStorage{}
	testHandler.Storage = store
	defer func() { testHandler.Storage = origStorage }()

	issueID, taskID := codeChangeFixture(t)

	var stored struct {
		Stored int `json:"stored"`
	}
	reportCodeChanges(t, taskID, codeChangeReportBody("run")).Want(http.StatusOK).JSON(&stored)
	if stored.Stored != 1 {
		t.Fatalf("stored = %d, want 1", stored.Stored)
	}

	// A retried upload of the same run is a no-op, not a second card.
	reportCodeChanges(t, taskID, codeChangeReportBody("run")).Want(http.StatusOK).JSON(&stored)
	if stored.Stored != 0 {
		t.Fatalf("retried upload stored = %d, want 0", stored.Stored)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_code_change WHERE task_id = $1`, taskID); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}

	var list struct {
		CodeChanges []TaskCodeChangeResponse `json:"code_changes"`
	}
	listReq := testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issueID+"/code-changes", nil), "id", issueID)
	testutil.Call(t, testHandler.ListIssueCodeChanges, listReq).Want(http.StatusOK).JSON(&list)
	if len(list.CodeChanges) != 1 {
		t.Fatalf("listed %d changes, want 1", len(list.CodeChanges))
	}
	change := list.CodeChanges[0]
	if change.TaskID != taskID || change.Scope != "run" || change.Additions != 2 || change.Deletions != 1 || !change.PatchAvailable {
		t.Fatalf("unexpected change: %+v", change)
	}
	if change.RepoURL != "https://github.com/acme/app.git" {
		t.Fatalf("repo_url = %q, want credentials stripped", change.RepoURL)
	}

	var detail TaskCodeChangeDetailResponse
	detailReq := testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issueID+"/code-changes/"+change.ID, nil), "id", issueID, "changeId", change.ID)
	testutil.Call(t, testHandler.GetIssueCodeChange, detailReq).Want(http.StatusOK).JSON(&detail)
	if detail.Patch == nil || *detail.Patch != samplePatch {
		t.Fatalf("patch = %v, want the uploaded patch", detail.Patch)
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != "main.go" || detail.Files[0].Additions != 2 {
		t.Fatalf("files = %+v", detail.Files)
	}
}

func TestGetIssueCodeChangeRejectsAnotherIssuesChange(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	_, taskID := codeChangeFixture(t)
	reportCodeChanges(t, taskID, codeChangeReportBody("run")).Want(http.StatusOK)
	var changeID string
	dbfx.QueryRow(t, `SELECT id::text FROM task_code_change WHERE task_id = $1`, taskID).Scan(&changeID)

	otherIssue := dbfx.Issue(t, "Unrelated issue")
	req := testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+otherIssue+"/code-changes/"+changeID, nil), "id", otherIssue, "changeId", changeID)
	testutil.Call(t, testHandler.GetIssueCodeChange, req).Want(http.StatusNotFound)
}

func TestReportTaskCodeChangesWithoutStorageKeepsFileList(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = nil
	defer func() { testHandler.Storage = origStorage }()

	_, taskID := codeChangeFixture(t)
	reportCodeChanges(t, taskID, codeChangeReportBody("branch")).Want(http.StatusOK)

	var omitted string
	var files int
	dbfx.QueryRow(t, `SELECT coalesce(patch_omitted, ''), jsonb_array_length(files) FROM task_code_change WHERE task_id = $1`, taskID).Scan(&omitted, &files)
	if omitted != "unavailable" || files != 1 {
		t.Fatalf("patch_omitted = %q, files = %d; want unavailable, 1", omitted, files)
	}
}

func TestReportTaskCodeChangesValidates(t *testing.T) {
	_, taskID := codeChangeFixture(t)

	bad := codeChangeReportBody("everything")
	reportCodeChanges(t, taskID, bad).Want(http.StatusBadRequest)

	badCommit := codeChangeReportBody("run")
	badCommit["changes"].([]map[string]any)[0]["head_commit"] = "HEAD; rm -rf /"
	reportCodeChanges(t, taskID, badCommit).Want(http.StatusBadRequest)

	if n := dbfx.Count(t, `SELECT count(*) FROM task_code_change WHERE task_id = $1`, taskID); n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}
}

func TestDeleteIssueRemovesItsCodeChanges(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	issueID, taskID := codeChangeFixture(t)
	reportCodeChanges(t, taskID, codeChangeReportBody("run")).Want(http.StatusOK)

	issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	result, err := testHandler.deleteIssueAndCollectAttachmentURLs(t.Context(), issue, nil)
	if err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_code_change WHERE issue_id = $1`, issueID); n != 0 {
		t.Fatalf("code change rows after issue delete = %d, want 0", n)
	}
	found := false
	for _, u := range result.AttachmentURLs {
		if strings.Contains(u, "/code-changes/") {
			found = true
		}
	}
	if !found {
		t.Fatalf("stored patch not scheduled for removal: %v", result.AttachmentURLs)
	}
}

func TestPullRequestDiffFromFiles(t *testing.T) {
	resp := pullRequestDiffFromFiles([]ghsnapshot.PullRequestFile{
		{Filename: "new.go", Status: "added", Additions: 1, Patch: "@@ -0,0 +1 @@\n+x"},
		{Filename: "b.go", PreviousFilename: "a.go", Status: "renamed", Additions: 1, Deletions: 1, Patch: "@@ -1 +1 @@\n-a\n+b"},
		{Filename: "logo.png", Status: "modified"},
	}, 1<<20)

	if resp.FileCount != 3 || resp.Additions != 2 || resp.Deletions != 1 {
		t.Fatalf("totals = %d files +%d -%d", resp.FileCount, resp.Additions, resp.Deletions)
	}
	if resp.Files[1].Status != "renamed" || resp.Files[1].OldPath != "a.go" {
		t.Fatalf("rename = %+v", resp.Files[1])
	}
	if !resp.Files[2].Binary {
		t.Fatal("a file GitHub gave no hunks and no line counts for should read as binary")
	}
	if resp.Patch == nil {
		t.Fatal("patch missing")
	}
	for _, want := range []string{
		"diff --git a/new.go b/new.go\nnew file mode 100644\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1 @@\n+x\n",
		"diff --git a/a.go b/b.go\nrename from a.go\nrename to b.go\n--- a/a.go\n+++ b/b.go\n",
	} {
		if !strings.Contains(*resp.Patch, want) {
			t.Fatalf("patch missing %q:\n%s", want, *resp.Patch)
		}
	}
	if strings.Contains(*resp.Patch, "logo.png") {
		t.Fatal("a file without hunks must stay out of the patch")
	}

	tooLarge := pullRequestDiffFromFiles([]ghsnapshot.PullRequestFile{
		{Filename: "big.go", Status: "modified", Additions: 1, Patch: "@@ -1 +1 @@\n+" + strings.Repeat("x", 64)},
	}, 16)
	if tooLarge.Patch != nil || tooLarge.PatchOmitted == nil || *tooLarge.PatchOmitted != "too_large" {
		t.Fatalf("oversized patch = %v / %v", tooLarge.Patch, tooLarge.PatchOmitted)
	}
}

func TestSanitizeRepoURL(t *testing.T) {
	cases := map[string]string{
		"https://user:token@github.com/acme/app.git?x=1": "https://github.com/acme/app.git",
		"git@github.com:acme/app.git":                    "git@github.com:acme/app.git",
		"":                                               "",
		"not a url":                                      "",
		"https:///nohost":                                "",
	}
	for in, want := range cases {
		if got := sanitizeRepoURL(in); got != want {
			t.Errorf("sanitizeRepoURL(%q) = %q, want %q", in, got, want)
		}
	}
}
