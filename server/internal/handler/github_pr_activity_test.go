package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type githubPRActivityFixture struct {
	fx             *testutil.Fixture
	issueID        string
	prID           string
	repo           string
	installationID int64
	closedAt       time.Time
}

func newGitHubPRActivityFixture(t *testing.T) *githubPRActivityFixture {
	t.Helper()
	installationID := randomInstallationID(t)
	wsID := dbfx.Workspace(t, "PR activity", fmt.Sprintf("pr-activity-%d", installationID))
	fx := testutil.New(testPool, wsID, testUserID)
	fx.Member(t, wsID, testUserID, "owner")
	fx.Insert(t, "github_installation", testutil.Cols{"installation_id": installationID, "workspace_id": wsID, "account_login": "acme"})
	issueID := fx.Issue(t, "PR activity")
	prID := fx.Insert(t, "github_pull_request", testutil.Cols{
		"workspace_id": wsID, "installation_id": installationID, "repo_owner": "acme", "repo_name": "widget", "pr_number": 7,
		"title": "Fix widget", "state": "open", "html_url": "https://github.com/acme/widget/pull/7",
		"head_sha": "head", "pr_created_at": time.Now(), "pr_updated_at": time.Now(),
	})
	fx.InsertNoID(t, "issue_pull_request", testutil.Cols{
		"issue_id": issueID, "pull_request_id": prID, "linked_by_type": "member", "linked_by_id": testUserID,
	}, "issue_id = $1 AND pull_request_id = $2", issueID, prID)
	fx.Cleanup(t, `DELETE FROM activity_log WHERE workspace_id = $1`, wsID)
	fx.Cleanup(t, `DELETE FROM issue_pr_automation WHERE issue_id = $1`, issueID)
	return &githubPRActivityFixture{fx: fx, issueID: issueID, prID: prID, repo: "widget", installationID: installationID, closedAt: time.Now().UTC().Truncate(time.Second).Add(time.Second)}
}

func githubPRActivityPayload(installationID int64, repo, lifecycle string, at time.Time) map[string]any {
	merged := lifecycle == "merged"
	state, action := "closed", "closed"
	var closedAt, mergedAt any = at.Format(time.RFC3339), nil
	if merged {
		mergedAt = closedAt
	}
	if lifecycle == "reopened" {
		state, action, closedAt = "open", "reopened", nil
	}
	return map[string]any{
		"action":       action,
		"installation": map[string]any{"id": installationID},
		"repository":   map[string]any{"name": repo, "owner": map[string]any{"login": "acme"}},
		"sender":       map[string]any{"id": 777, "login": "octocat"},
		"pull_request": map[string]any{
			"number": 7, "html_url": "https://github.com/acme/" + repo + "/pull/7", "title": "Fix widget", "state": state,
			"merged": merged, "merged_at": mergedAt, "closed_at": closedAt,
			"created_at": at.Add(-time.Hour).Format(time.RFC3339), "updated_at": at.Format(time.RFC3339),
			"head":      map[string]any{"ref": "fix/widget", "sha": "head"},
			"user":      map[string]any{"id": 555, "login": "pr-author"},
			"merged_by": map[string]any{"id": 777, "login": "octocat"},
		},
	}
}

func deliverGitHubPRActivity(t *testing.T, payload map[string]any) {
	t.Helper()
	const secret = "pr-activity-webhook-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(raw))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	testutil.Call(t, testHandler.HandleGitHubWebhook, req).Want(http.StatusAccepted)
}

func githubPRTimelineActivities(t *testing.T, wsID, issueID string) []TimelineEntry {
	t.Helper()
	req := withURLParam(newRequest(http.MethodGet, "/api/issues/"+issueID+"/timeline", nil), "id", issueID)
	req.Header.Set("X-Workspace-ID", wsID)
	var all []TimelineEntry
	testutil.Call(t, testHandler.ListTimeline, req).Want(http.StatusOK).JSON(&all)
	var activities []TimelineEntry
	for _, entry := range all {
		if entry.Action != nil && (*entry.Action == "pull_request_merged" || *entry.Action == "pull_request_closed") {
			activities = append(activities, entry)
		}
	}
	return activities
}

func TestGitHubPRWebhookActivityExternalActors(t *testing.T) {
	for _, lifecycle := range []string{"merged", "closed"} {
		t.Run(lifecycle, func(t *testing.T) {
			f := newGitHubPRActivityFixture(t)
			payload := githubPRActivityPayload(f.installationID, f.repo, lifecycle, f.closedAt)
			payload["sender"] = map[string]any{"id": 888, "login": "external-closer"}
			payload["pull_request"].(map[string]any)["merged_by"] = map[string]any{"id": 999, "login": "external-merger"}
			deliverGitHubPRActivity(t, payload)
			deliverGitHubPRActivity(t, payload)
			entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, f.issueID)
			if len(entries) != 1 || *entries[0].Action != "pull_request_"+lifecycle {
				t.Fatalf("activities = %+v", entries)
			}
			wantActor := "@external-closer (GitHub)"
			if lifecycle == "merged" {
				wantActor = "@external-merger (GitHub)"
			}
			entry := entries[0]
			createdAt, err := time.Parse(time.RFC3339, entry.CreatedAt)
			if err != nil || !createdAt.Equal(f.closedAt) || entry.ActorType != "system" || entry.ActorID != "" || entry.ActorName != wantActor {
				t.Fatalf("external actor/time = %+v", entry)
			}
			if details := decodeBody(t, entry.Details); details["pull_request_id"] != f.prID || details["source"] != "github" {
				t.Fatalf("details = %v", details)
			}
		})
	}
}

func TestGitHubPRWebhookActivityRequiresSignature(t *testing.T) {
	f := newGitHubPRActivityFixture(t)
	t.Setenv("GITHUB_WEBHOOK_SECRET", "pr-activity-webhook-secret")
	raw, err := json.Marshal(githubPRActivityPayload(f.installationID, f.repo, "closed", f.closedAt))
	if err != nil {
		t.Fatal(err)
	}
	for _, signature := range []string{"", "sha256=deadbeef"} {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(raw))
		req.Header.Set("X-GitHub-Event", "pull_request")
		req.Header.Set("X-Hub-Signature-256", signature)
		testutil.Call(t, testHandler.HandleGitHubWebhook, req).Want(http.StatusUnauthorized)
	}
	if entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, f.issueID); len(entries) != 0 {
		t.Fatalf("unverified webhook recorded an activity: %+v", entries)
	}
}

func TestGitHubPRWebhookActivityActorMembership(t *testing.T) {
	f := newGitHubPRActivityFixture(t)
	f.fx.InsertNoID(t, "github_user_connection", testutil.Cols{
		"user_id": testUserID, "github_user_id": 777, "login": "old-login", "access_token_encrypted": []byte("unused"),
	}, "user_id = $1", testUserID)
	payload := githubPRActivityPayload(f.installationID, f.repo, "merged", f.closedAt)
	payload["sender"] = map[string]any{"id": 888, "login": "github-bot"}
	deliverGitHubPRActivity(t, payload)
	entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, f.issueID)
	if len(entries) != 1 || entries[0].ActorType != "member" || entries[0].ActorID != testUserID {
		t.Fatalf("member attribution = %+v", entries)
	}

	otherUserID := f.fx.User(t, "Other member", fmt.Sprintf("activity-member-%d@example.com", f.installationID))
	f.fx.Member(t, f.fx.WorkspaceID, otherUserID, "member")
	f.fx.InsertNoID(t, "github_user_connection", testutil.Cols{
		"user_id": otherUserID, "github_user_id": 777, "login": "octocat", "access_token_encrypted": []byte("unused"),
	}, "user_id = $1", otherUserID)
	deliverGitHubPRActivity(t, githubPRActivityPayload(f.installationID, f.repo, "closed", f.closedAt.Add(time.Second)))
	entries = githubPRTimelineActivities(t, f.fx.WorkspaceID, f.issueID)
	if len(entries) != 2 || entries[1].ActorType != "system" || entries[1].ActorID != "" {
		t.Fatalf("ambiguous account must not pick a member: %+v", entries)
	}
}

func TestGitHubPRWebhookActivityNonMemberActor(t *testing.T) {
	f := newGitHubPRActivityFixture(t)
	outsiderID := dbfx.User(t, "Other workspace member", fmt.Sprintf("activity-outsider-%d@example.com", f.installationID))
	dbfx.Member(t, testWorkspaceID, outsiderID, "member")
	dbfx.InsertNoID(t, "github_user_connection", testutil.Cols{
		"user_id": outsiderID, "github_user_id": 777, "login": "octocat", "access_token_encrypted": []byte("unused"),
	}, "user_id = $1", outsiderID)
	deliverGitHubPRActivity(t, githubPRActivityPayload(f.installationID, f.repo, "closed", f.closedAt))
	entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, f.issueID)
	if len(entries) != 1 || entries[0].ActorType != "system" || entries[0].ActorID != "" || entries[0].ActorName != "@octocat (GitHub)" {
		t.Fatalf("nonmember must remain a GitHub actor: %+v", entries)
	}
}

func TestGitHubPRWebhookActivityFanoutAndIsolation(t *testing.T) {
	f := newGitHubPRActivityFixture(t)
	second := f.fx.Issue(t, "Second linked issue")
	linkTestPR(t, second, f.prID)
	foreign := dbfx.Issue(t, "Other workspace")
	linkTestPR(t, foreign, f.prID)
	unlinked := f.fx.Issue(t, "Unlinked issue")
	excluded := f.fx.Issue(t, "Excluded issue")
	linkTestPR(t, excluded, f.prID)
	f.fx.InsertNoID(t, "issue_pull_request_exclusion", testutil.Cols{"workspace_id": f.fx.WorkspaceID, "issue_id": excluded, "pull_request_id": f.prID}, "issue_id = $1 AND pull_request_id = $2", excluded, f.prID)
	f.fx.Cleanup(t, `DELETE FROM activity_log WHERE workspace_id = $1`, f.fx.WorkspaceID)
	f.fx.Cleanup(t, `DELETE FROM issue_pr_automation WHERE issue_id = ANY($1::uuid[])`, []string{second, unlinked, excluded})
	deliverGitHubPRActivity(t, githubPRActivityPayload(f.installationID, f.repo, "closed", f.closedAt))
	for _, id := range []string{f.issueID, second} {
		if entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, id); len(entries) != 1 {
			t.Fatalf("linked issue %s activities = %+v", id, entries)
		}
	}
	for _, id := range []string{unlinked, excluded} {
		if entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, id); len(entries) != 0 {
			t.Fatalf("unlinked/excluded issue %s activities = %+v", id, entries)
		}
	}
	if entries := githubPRTimelineActivities(t, testWorkspaceID, foreign); len(entries) != 0 {
		t.Fatalf("cross-workspace link activities = %+v", entries)
	}
}

func TestGitHubPRWebhookActivityRedeliveryAndReopen(t *testing.T) {
	f := newGitHubPRActivityFixture(t)
	first := githubPRActivityPayload(f.installationID, f.repo, "closed", f.closedAt)
	deliverGitHubPRActivity(t, first)
	deliverGitHubPRActivity(t, githubPRActivityPayload(f.installationID, f.repo, "reopened", f.closedAt.Add(time.Second)))
	second := githubPRActivityPayload(f.installationID, f.repo, "closed", f.closedAt.Add(2*time.Second))
	deliverGitHubPRActivity(t, second)
	deliverGitHubPRActivity(t, first)
	second["action"] = "edited"
	deliverGitHubPRActivity(t, second)
	second["action"] = "closed"
	raw, _ := json.Marshal(second)
	var deliveries sync.WaitGroup
	for range 4 {
		deliveries.Go(func() { testHandler.handlePullRequestEvent(context.Background(), raw) })
	}
	deliveries.Wait()
	entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, f.issueID)
	if len(entries) != 2 || entries[0].ID == entries[1].ID {
		t.Fatalf("repeated/reopened activities = %+v", entries)
	}
}

func TestGitHubPRWebhookActivityLateCloseAndDisabledWorkspace(t *testing.T) {
	f := newGitHubPRActivityFixture(t)
	closed := githubPRActivityPayload(f.installationID, f.repo, "closed", f.closedAt)
	edited := githubPRActivityPayload(f.installationID, f.repo, "closed", f.closedAt.Add(time.Second))
	edited["action"] = "edited"
	deliverGitHubPRActivity(t, edited)
	if entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, f.issueID); len(entries) != 0 {
		t.Fatalf("metadata update must not create an activity: %+v", entries)
	}
	deliverGitHubPRActivity(t, closed)
	if entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, f.issueID); len(entries) != 1 {
		t.Fatalf("late close activities = %+v", entries)
	}
	f.fx.Exec(t, `UPDATE workspace SET settings = settings || '{"github_enabled":false}'::jsonb WHERE id = $1`, f.fx.WorkspaceID)
	deliverGitHubPRActivity(t, githubPRActivityPayload(f.installationID, f.repo, "closed", f.closedAt.Add(2*time.Second)))
	if entries := githubPRTimelineActivities(t, f.fx.WorkspaceID, f.issueID); len(entries) != 1 {
		t.Fatalf("disabled workspace recorded an activity: %+v", entries)
	}
}
