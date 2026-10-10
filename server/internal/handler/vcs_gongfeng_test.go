package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestVCSWebhook_GongfengLifecycle(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	sealed, err := box.Seal([]byte(vcsTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	connID := dbfx.Insert(t, "vcs_connection", testutil.Cols{
		"workspace_id": testWorkspaceID, "provider": "gongfeng",
		"instance_url": "https://git.code.tencent.com", "account_login": "alice",
		"access_token_encrypted":   base64.StdEncoding.EncodeToString(sealed),
		"webhook_secret_encrypted": base64.StdEncoding.EncodeToString(sealed),
	})
	t.Cleanup(func() {
		if err := testHandler.Queries.DeleteVCSConnection(ctx, db.DeleteVCSConnectionParams{
			ID: parseUUID(connID), WorkspaceID: parseUUID(testWorkspaceID),
		}); err != nil {
			t.Errorf("cleanup connection: %v", err)
		}
	})
	issueID := dbfx.Issue(t, "Gongfeng lifecycle", testutil.Cols{"status": "in_progress"})
	dbfx.Cleanup(t, "DELETE FROM activity_log WHERE issue_id = $1", issueID)
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := testHandler.Queries.GetWorkspace(ctx, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	identifier := fmt.Sprintf("%s-%d", ws.IssuePrefix, issue.Number)
	fixture, err := os.ReadFile("../integrations/vcs/testdata/gongfeng-merge-request.json")
	if err != nil {
		t.Fatal(err)
	}

	fire := func(number int, action, state, actor, body, updatedAt, token string, status int) {
		t.Helper()
		var payload map[string]any
		if err := json.Unmarshal(fixture, &payload); err != nil {
			t.Fatal(err)
		}
		a := payload["object_attributes"].(map[string]any)
		a["iid"], a["action"], a["state"], a["description"], a["updated_at"] = number, action, state, body, updatedAt
		a["url"] = fmt.Sprintf("https://git.code.tencent.com/acme/team/widget/merge_requests/%d", number)
		// A body closing reference alone must link, regardless of the title/branch.
		a["title"], a["source_branch"] = "Fix login", "fix/login"
		payload["user"].(map[string]any)["username"] = actor
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		testutil.Call(t, testHandler.HandleVCSWebhook, vcsWebhookReq(connID, map[string]string{
			"X-Event": "Merge Request Hook", "X-Token": token,
		}, raw)).Want(status)
	}
	check := func(wantStates map[int32]string, wantIssueStatus string) {
		t.Helper()
		rows, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issueID))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != len(wantStates) {
			t.Fatalf("linked MRs = %d, want %d", len(rows), len(wantStates))
		}
		for _, row := range rows {
			if row.State != wantStates[row.PrNumber] || row.Provider != "gongfeng" || row.RepoOwner != "acme/team" || row.RepoName != "widget" || row.AuthorLogin.String != "alice" {
				t.Fatalf("unexpected mirrored MR: %+v", row)
			}
		}
		got, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != wantIssueStatus {
			t.Fatalf("issue status = %q, want %q", got.Status, wantIssueStatus)
		}
	}
	closing := "Closes " + identifier
	fire(7, "open", "opened", "alice", closing, "2026-10-09T08:01:00+0000", "wrong", 401)
	check(map[int32]string{}, "in_progress")
	fire(7, "open", "opened", "alice", closing, "2026-10-09T08:01:00+0000", vcsTestSecret, 202)
	fire(7, "open", "opened", "alice", closing, "2026-10-09T08:01:00+0000", vcsTestSecret, 202)
	fire(8, "open", "opened", "alice", closing, "2026-10-09T08:01:00+0000", vcsTestSecret, 202)
	// The same mirrored MR is also linkable by its official URL. Unlinking
	// remembers the exclusion, and explicit linking can restore it.
	manualIssueID := dbfx.Issue(t, "Manually linked Gongfeng MR")
	dbfx.Cleanup(t, "DELETE FROM activity_log WHERE issue_id = $1", manualIssueID)
	link := func() {
		t.Helper()
		req := testutil.WithURLParams(newRequest("POST", "/api/issues/"+manualIssueID+"/pull-requests", map[string]any{
			"url": "https://git.code.tencent.com/acme/team/widget/merge_requests/7/diffs?view=inline",
		}), "id", manualIssueID)
		testutil.Call(t, testHandler.LinkIssuePullRequest, req).Want(200)
	}
	link()
	rows, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(manualIssueID))
	if err != nil || len(rows) != 1 {
		t.Fatalf("manual Gongfeng link: rows=%+v, err=%v", rows, err)
	}
	listed := listIssuePRsForTest(t, manualIssueID)
	if len(listed.PullRequests) != 1 || listed.PullRequests[0].LinkSource != "manual" {
		t.Fatalf("manual link not exposed by API: %+v", listed.PullRequests)
	}
	prID := uuidToString(rows[0].ID)
	req := testutil.WithURLParams(newRequest("DELETE", "/api/issues/"+manualIssueID+"/pull-requests/"+prID, nil), "id", manualIssueID, "prId", prID)
	testutil.Call(t, testHandler.UnlinkIssuePullRequest, req).Want(200)
	if got, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(manualIssueID)); err != nil || len(got) != 0 {
		t.Fatalf("unlinked Gongfeng MR still visible: rows=%+v, err=%v", got, err)
	}
	link()
	fire(7, "close", "closed", "bob", "", "2026-10-09T08:02:00+0000", vcsTestSecret, 202)
	check(map[int32]string{7: "closed", 8: "open"}, "in_progress")
	fire(7, "reopen", "reopened", "bob", closing, "2026-10-09T08:03:00+0000", vcsTestSecret, 202)
	check(map[int32]string{7: "open", 8: "open"}, "in_progress")
	fire(7, "merge", "merged", "bob", "", "2026-10-09T08:04:00.123456+0000", vcsTestSecret, 202)
	check(map[int32]string{7: "merged", 8: "open"}, "in_progress")
	// Stale delivery must not regress the MR or remove its link.
	fire(7, "update", "opened", "bob", "", "2026-10-09T08:04:00.123455+0000", vcsTestSecret, 202)
	check(map[int32]string{7: "merged", 8: "open"}, "in_progress")
	fire(8, "merge", "merged", "bob", "", "2026-10-09T08:05:00+0000", vcsTestSecret, 202)
	check(map[int32]string{7: "merged", 8: "merged"}, "done")
}
