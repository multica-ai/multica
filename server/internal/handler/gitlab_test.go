package handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGitLabUserSystemKey(t *testing.T) {
	if got := gitlabUserSystemKey(4242, "alice"); got != "gitlab:4242" {
		t.Fatalf("id key: got %q", got)
	}
	if got := gitlabUserSystemKey(0, "Alice"); got != "gitlab:u:alice" {
		t.Fatalf("username key: got %q", got)
	}
	if got := gitlabUserSystemKey(0, "  "); got != "" {
		t.Fatalf("empty key: got %q", got)
	}
}

func TestWorkspaceGitLabIssueSyncLabel(t *testing.T) {
	if got := workspaceGitLabIssueSyncLabel(nil); got != "agent" {
		t.Fatalf("nil settings: got %q", got)
	}
	if got := workspaceGitLabIssueSyncLabel([]byte(`{}`)); got != "agent" {
		t.Fatalf("empty object: got %q", got)
	}
	if got := workspaceGitLabIssueSyncLabel([]byte(`{"gitlab_issue_sync_label":""}`)); got != "agent" {
		t.Fatalf("blank label: got %q", got)
	}
	if got := workspaceGitLabIssueSyncLabel([]byte(`{"gitlab_issue_sync_label":"  multica  "}`)); got != "multica" {
		t.Fatalf("custom label: got %q", got)
	}
}

func TestGitLabLabelAgentNameCandidates(t *testing.T) {
	labels := []struct {
		Title string `json:"title"`
	}{
		{Title: "agent"},
		{Title: "agent::Coder"},
		{Title: "  Research  "},
	}
	got := gitlabLabelAgentNameCandidates(labels, "agent")
	want := []string{"agent", "agent::Coder", "Coder", "Research"}
	if len(got) != len(want) {
		t.Fatalf("candidates: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHasGitLabIssueSyncTrigger(t *testing.T) {
	label := func(titles ...string) []struct {
		Title string `json:"title"`
	} {
		out := make([]struct {
			Title string `json:"title"`
		}, len(titles))
		for i, t := range titles {
			out[i].Title = t
		}
		return out
	}

	if !hasGitLabIssueSyncTrigger(label("agent"), "agent") {
		t.Fatal("bare sync label should trigger")
	}
	if !hasGitLabIssueSyncTrigger(label("agent::Implementer"), "agent") {
		t.Fatal("prefixed agent name alone should trigger import")
	}
	if !hasGitLabIssueSyncTrigger(label("bug", "agent::Coder"), "agent") {
		t.Fatal("prefixed label among others should trigger")
	}
	if hasGitLabIssueSyncTrigger(label("agent:"), "agent") {
		t.Fatal("single-colon form should not trigger")
	}
	if hasGitLabIssueSyncTrigger(label("agent::"), "agent") {
		t.Fatal("empty name after prefix should not trigger")
	}
	if hasGitLabIssueSyncTrigger(label("Implementer"), "agent") {
		t.Fatal("agent name alone without sync prefix should not trigger")
	}
	if hasGitLabIssueSyncTrigger(label("agents"), "agent") {
		t.Fatal("unrelated label should not trigger")
	}
}

func TestMatchAgentByGitLabLabels(t *testing.T) {
	coderID := parseUUID("11111111-1111-1111-1111-111111111111")
	researchID := parseUUID("22222222-2222-2222-2222-222222222222")
	agents := []db.Agent{
		{ID: coderID, Name: "Coder", Kind: "user"},
		{ID: researchID, Name: "Research", Kind: "user"},
		{ID: parseUUID("33333333-3333-3333-3333-333333333333"), Name: "Persona", Kind: "system"},
	}
	label := func(titles ...string) []struct {
		Title string `json:"title"`
	} {
		out := make([]struct {
			Title string `json:"title"`
		}, len(titles))
		for i, t := range titles {
			out[i].Title = t
		}
		return out
	}

	if _, ok := matchAgentByGitLabLabels(agents, label("agent"), "agent"); ok {
		t.Fatal("sync label alone should not assign when no agent is named agent")
	}
	got, ok := matchAgentByGitLabLabels(agents, label("agent", "agent::Coder"), "agent")
	if !ok || uuidToString(got.ID) != uuidToString(coderID) {
		t.Fatalf("prefixed name: ok=%v agent=%q", ok, got.Name)
	}
	got, ok = matchAgentByGitLabLabels(agents, label("agent", "Research"), "agent")
	if !ok || uuidToString(got.ID) != uuidToString(researchID) {
		t.Fatalf("exact name: ok=%v agent=%q", ok, got.Name)
	}
	if _, ok := matchAgentByGitLabLabels(agents, label("agent", "Coder", "Research"), "agent"); ok {
		t.Fatal("two agent-name labels should be ambiguous")
	}
	if _, ok := matchAgentByGitLabLabels(agents, label("agent", "Persona"), "agent"); ok {
		t.Fatal("system persona must not be assigned")
	}
	got, ok = matchAgentByGitLabLabels(agents, label("agent", "coder"), "agent")
	if !ok || uuidToString(got.ID) != uuidToString(coderID) {
		t.Fatalf("case-insensitive: ok=%v agent=%q", ok, got.Name)
	}
}

func TestSplitGitRemote(t *testing.T) {
	tests := []struct {
		raw      string
		wantHost string
		wantPath string
	}{
		{"https://git.example.com/group/repo.git", "git.example.com", "group/repo"},
		{"https://git.example.com/group/repo", "git.example.com", "group/repo"},
		{"git@git.example.com:group/repo.git", "git.example.com", "group/repo"},
		{"ssh://git@git.example.com/group/sub/repo.git", "git.example.com", "group/sub/repo"},
		{"group/repo", "", "group/repo"},
		{"https://git.example.com:8443/Group/Repo.GIT", "git.example.com", "group/repo"},
		{"", "", ""},
	}
	for _, tc := range tests {
		host, path := splitGitRemote(tc.raw)
		if host != tc.wantHost || path != tc.wantPath {
			t.Errorf("splitGitRemote(%q) = (%q, %q), want (%q, %q)",
				tc.raw, host, path, tc.wantHost, tc.wantPath)
		}
	}
}

func TestGitRepoMatchesGitLabProject(t *testing.T) {
	pathKey := "paral/app"
	candidates := []string{
		"https://git.paral.no/paral/app.git",
		"git@git.paral.no:paral/app.git",
	}

	if !gitRepoMatchesGitLabProject("https://git.paral.no/paral/app", pathKey, candidates) {
		t.Error("expected https resource URL to match candidate")
	}
	if !gitRepoMatchesGitLabProject("git@git.paral.no:paral/app.git", pathKey, candidates) {
		t.Error("expected scp-style resource URL to match candidate")
	}
	// Path-only fallback when candidates empty.
	if !gitRepoMatchesGitLabProject("https://git.paral.no/paral/app.git", pathKey, nil) {
		t.Error("expected path-only match against path_with_namespace")
	}
	// Different host with same path and explicit candidates should not match.
	if gitRepoMatchesGitLabProject("https://github.com/paral/app.git", pathKey, candidates) {
		t.Error("expected different host to not match when candidates carry host")
	}
	// Wrong path.
	if gitRepoMatchesGitLabProject("https://git.paral.no/paral/other.git", pathKey, candidates) {
		t.Error("expected different path to not match")
	}
}

func TestGitlabProjectCandidateURLs(t *testing.T) {
	t.Setenv("GITLAB_URL", "https://git.paral.no")
	p := gitlabWebhookProject{
		PathWithNamespace: "paral/app",
		WebURL:            "https://git.paral.no/paral/app",
		GitHTTPURL:        "https://git.paral.no/paral/app.git",
		GitSSHURL:         "git@git.paral.no:paral/app.git",
	}
	got := gitlabProjectCandidateURLs(p)
	wantAny := []string{
		"https://git.paral.no/paral/app.git",
		"git@git.paral.no:paral/app.git",
		"https://git.paral.no/paral/app",
	}
	for _, w := range wantAny {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("candidate URLs missing %q; got %v", w, got)
		}
	}
}

func TestHandleGitLabWebhook_MissingSecret(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	// No env secret and no connection → reject (unknown namespace, no legacy fallback).
	t.Setenv("GITLAB_WEBHOOK_SECRET", "")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(`{"project":{"namespace":"nope"}}`))
	req.Header.Set("X-Gitlab-Token", "anything")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleGitLabWebhook_WrongToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "wrong-token-ns",
		NamespaceType: "group",
		AccessToken:   "dummy",
		WebhookSecret: "correct-secret",
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})
	t.Setenv("GITLAB_WEBHOOK_SECRET", "")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(
		`{"project":{"namespace":"wrong-token-ns"}}`,
	))
	req.Header.Set("X-Gitlab-Token", "wrong")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleGitLabWebhook_UnknownEvent(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "unknown-event-ns",
		NamespaceType: "group",
		AccessToken:   "dummy",
		WebhookSecret: "s",
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})
	t.Setenv("GITLAB_WEBHOOK_SECRET", "")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(
		`{"project":{"namespace":"unknown-event-ns"}}`,
	))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// TestHandleGitLabIssueEvent_LabelAdd tests that an Issue Hook with the
// "agent" label on a new issue creates a Multica issue and gitlab_issue row.
func TestHandleGitLabIssueEvent_LabelAdd(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	// Create a GitLab connection for the test workspace.
	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-add",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	payload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 10, "title": "Sync me", "description": "desc", "action": "open"},
		"project": {"id": 99, "path_with_namespace": "testorg-issue-add/repo", "namespace": "testorg-issue-add"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Verify gitlab_issue row was created.
	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID,
		ProjectPath: "testorg-issue-add/repo",
		GlIssueIid:  10,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not created: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("multica issue not created: %v", err)
	}
	if issue.Title != "Sync me" {
		t.Errorf("title: got %q, want %q", issue.Title, "Sync me")
	}
	if issue.AssigneeType.Valid {
		t.Errorf("expected unassigned without agent-name label, got type=%q", issue.AssigneeType.String)
	}
}

// TestHandleGitLabIssueEvent_AssignsAgentByName assigns the Multica agent whose
// name matches a GitLab label (exact or "{syncLabel}::{name}").
func TestHandleGitLabIssueEvent_AssignsAgentByName(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)

	// Seeded fixture agent is named "Handler Test Agent".
	agents, err := testHandler.Queries.ListAgents(ctx, wsUUID)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	var seeded db.Agent
	for _, a := range agents {
		if a.Name == "Handler Test Agent" && a.Kind == "user" {
			seeded = a
			break
		}
	}
	if !seeded.ID.Valid {
		t.Fatal("setup: fixture agent Handler Test Agent not found")
	}

	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-agent-name",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Prefixed form alone (no bare "agent" label) must still import + assign.
	payload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 88, "title": "Assign me", "description": "", "action": "open"},
		"project": {"id": 888, "path_with_namespace": "testorg-agent-name/repo", "namespace": "testorg-agent-name"},
		"labels": [{"title": "agent::Handler Test Agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-agent-name/repo", GlIssueIid: 88,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not created: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("multica issue not created: %v", err)
	}
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" {
		t.Fatalf("assignee_type: got %v, want agent", issue.AssigneeType)
	}
	if !issue.AssigneeID.Valid || uuidToString(issue.AssigneeID) != uuidToString(seeded.ID) {
		t.Fatalf("assignee_id: got %v, want %s", issue.AssigneeID, uuidToString(seeded.ID))
	}

	// Exact agent name still requires the bare sync label (name alone is not a trigger).
	payload2 := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 89, "title": "Assign exact", "description": "", "action": "open"},
		"project": {"id": 888, "path_with_namespace": "testorg-agent-name/repo", "namespace": "testorg-agent-name"},
		"labels": [{"title": "agent"}, {"title": "Handler Test Agent"}],
		"assignees": []
	}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload2))
	req2.Header.Set("X-Gitlab-Token", "s")
	req2.Header.Set("X-Gitlab-Event", "Issue Hook")
	w2 := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("exact: expected 204, got %d", w2.Code)
	}
	row2, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-agent-name/repo", GlIssueIid: 89,
	})
	if err != nil {
		t.Fatalf("exact: gitlab_issue not created: %v", err)
	}
	issue2, err := testHandler.Queries.GetIssue(ctx, row2.IssueID)
	if err != nil {
		t.Fatalf("exact: issue not created: %v", err)
	}
	if !issue2.AssigneeID.Valid || uuidToString(issue2.AssigneeID) != uuidToString(seeded.ID) {
		t.Fatalf("exact assignee_id: got %v, want %s", issue2.AssigneeID, uuidToString(seeded.ID))
	}

	// Unknown agent name with prefix: still import, leave unassigned.
	payload3 := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 90, "title": "Unknown agent", "description": "", "action": "open"},
		"project": {"id": 888, "path_with_namespace": "testorg-agent-name/repo", "namespace": "testorg-agent-name"},
		"labels": [{"title": "agent::NoSuchAgent"}],
		"assignees": []
	}`
	req3 := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload3))
	req3.Header.Set("X-Gitlab-Token", "s")
	req3.Header.Set("X-Gitlab-Event", "Issue Hook")
	w3 := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w3, req3)
	if w3.Code != http.StatusNoContent {
		t.Fatalf("unknown: expected 204, got %d", w3.Code)
	}
	row3, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-agent-name/repo", GlIssueIid: 90,
	})
	if err != nil {
		t.Fatalf("unknown: gitlab_issue not created: %v", err)
	}
	issue3, err := testHandler.Queries.GetIssue(ctx, row3.IssueID)
	if err != nil {
		t.Fatalf("unknown: issue not created: %v", err)
	}
	if issue3.AssigneeType.Valid {
		t.Fatalf("unknown agent should leave issue unassigned, got type=%q id=%v",
			issue3.AssigneeType.String, issue3.AssigneeID)
	}
}

// TestHandleGitLabIssueEvent_AssignsMatchingProject creates the Multica issue
// under the project whose github_repo resource matches the GitLab repo URL.
func TestHandleGitLabIssueEvent_AssignsMatchingProject(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)

	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-project",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Matching Multica project + github_repo resource for the GitLab path.
	project, err := testHandler.Queries.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID: wsUUID,
		Title:       "GitLab-matched project",
		Status:      "planned",
		Priority:    "none",
	})
	if err != nil {
		t.Fatalf("setup: create project: %v", err)
	}
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteProject(ctx, db.DeleteProjectParams{
			ID: project.ID, WorkspaceID: wsUUID,
		})
	})

	// Decoy project with a different repo — must not win.
	decoy, err := testHandler.Queries.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID: wsUUID,
		Title:       "Decoy project",
		Status:      "planned",
		Priority:    "none",
	})
	if err != nil {
		t.Fatalf("setup: create decoy project: %v", err)
	}
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteProject(ctx, db.DeleteProjectParams{
			ID: decoy.ID, WorkspaceID: wsUUID,
		})
	})

	_, err = testHandler.Queries.CreateProjectResource(ctx, db.CreateProjectResourceParams{
		ProjectID:    project.ID,
		WorkspaceID:  wsUUID,
		ResourceType: "github_repo",
		ResourceRef:  []byte(`{"url":"https://git.example.com/testorg-issue-project/repo.git"}`),
		Position:     0,
	})
	if err != nil {
		t.Fatalf("setup: create matching resource: %v", err)
	}
	_, err = testHandler.Queries.CreateProjectResource(ctx, db.CreateProjectResourceParams{
		ProjectID:    decoy.ID,
		WorkspaceID:  wsUUID,
		ResourceType: "github_repo",
		ResourceRef:  []byte(`{"url":"https://git.example.com/testorg-issue-project/other.git"}`),
		Position:     0,
	})
	if err != nil {
		t.Fatalf("setup: create decoy resource: %v", err)
	}

	payload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 77, "title": "Route me", "description": "to project", "action": "open"},
		"project": {
			"id": 777,
			"path_with_namespace": "testorg-issue-project/repo",
			"namespace": "testorg-issue-project",
			"web_url": "https://git.example.com/testorg-issue-project/repo",
			"git_http_url": "https://git.example.com/testorg-issue-project/repo.git",
			"git_ssh_url": "git@git.example.com:testorg-issue-project/repo.git"
		},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID,
		ProjectPath: "testorg-issue-project/repo",
		GlIssueIid:  77,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not created: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("multica issue not created: %v", err)
	}
	if !issue.ProjectID.Valid {
		t.Fatal("expected issue.ProjectID to be set from matching project resource")
	}
	if uuidToString(issue.ProjectID) != uuidToString(project.ID) {
		t.Errorf("issue.ProjectID = %s, want %s", uuidToString(issue.ProjectID), uuidToString(project.ID))
	}

	// Cleanup created issue (gitlab_issue cascades / explicit delete).
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteIssue(ctx, db.DeleteIssueParams{
			ID: issue.ID, WorkspaceID: wsUUID,
		})
	})
}

// TestHandleGitLabIssueEvent_CustomSyncLabel creates a Multica issue when the
// workspace configures a non-default GitLab label.
func TestHandleGitLabIssueEvent_CustomSyncLabel(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)

	var previousSettings []byte
	if err := testPool.QueryRow(ctx, `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previousSettings); err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET settings = $1::jsonb WHERE id = $2`,
		`{"gitlab_issue_sync_label":"multica"}`, testWorkspaceID); err != nil {
		t.Fatalf("set settings: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1::jsonb WHERE id = $2`, previousSettings, testWorkspaceID)
	})

	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-custom-label",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Default "agent" label must not create when custom label is configured.
	agentOnly := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 50, "title": "Wrong label", "description": "", "action": "open"},
		"project": {"id": 500, "path_with_namespace": "testorg-custom-label/repo", "namespace": "testorg-custom-label"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(agentOnly))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if _, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-custom-label/repo", GlIssueIid: 50,
	}); err == nil {
		t.Fatal("expected no gitlab_issue for default agent label when custom label configured")
	}

	customPayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 51, "title": "Custom label sync", "description": "hi", "action": "open"},
		"project": {"id": 500, "path_with_namespace": "testorg-custom-label/repo", "namespace": "testorg-custom-label"},
		"labels": [{"title": "multica"}],
		"assignees": []
	}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(customPayload))
	req2.Header.Set("X-Gitlab-Token", "s")
	req2.Header.Set("X-Gitlab-Event", "Issue Hook")
	w2 := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w2.Code)
	}
	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-custom-label/repo", GlIssueIid: 51,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not created for custom label: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("multica issue not created: %v", err)
	}
	if issue.Title != "Custom label sync" {
		t.Errorf("title: got %q, want %q", issue.Title, "Custom label sync")
	}
}

// TestHandleGitLabIssueEvent_LabelRemoveKeepsIssue tests that removing the
// "agent" label does not delete the linked Multica issue or gitlab_issue row.
func TestHandleGitLabIssueEvent_LabelRemoveKeepsIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-remove",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Seed: create issue+row via the add path.
	addPayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 11, "title": "Remove me", "description": "", "action": "open"},
		"project": {"id": 100, "path_with_namespace": "testorg-issue-remove/repo", "namespace": "testorg-issue-remove"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	addReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(addPayload))
	addReq.Header.Set("X-Gitlab-Token", "s")
	addReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), addReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-remove/repo", GlIssueIid: 11,
	})
	if err != nil {
		t.Fatalf("seed: gitlab_issue not found: %v", err)
	}
	issueID := row.IssueID

	// Remove label.
	removePayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 11, "title": "Remove me", "description": "", "action": "update"},
		"project": {"id": 100, "path_with_namespace": "testorg-issue-remove/repo", "namespace": "testorg-issue-remove"},
		"labels": [],
		"assignees": []
	}`
	removeReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(removePayload))
	removeReq.Header.Set("X-Gitlab-Token", "s")
	removeReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), removeReq)

	// Multica issue and gitlab_issue link must remain.
	if _, err := testHandler.Queries.GetIssue(ctx, issueID); err != nil {
		t.Fatalf("multica issue should remain after agent label removal: %v", err)
	}
	if _, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-remove/repo", GlIssueIid: 11,
	}); err != nil {
		t.Fatalf("gitlab_issue row should remain after agent label removal: %v", err)
	}
}

// TestHandleGitLabIssueEvent_Close tests that action=close marks the issue Done.
func TestHandleGitLabIssueEvent_Close(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-close",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Seed.
	addPayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 12, "title": "Close me", "description": "", "action": "open"},
		"project": {"id": 101, "path_with_namespace": "testorg-issue-close/repo", "namespace": "testorg-issue-close"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	addReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(addPayload))
	addReq.Header.Set("X-Gitlab-Token", "s")
	addReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), addReq)

	row, _ := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-close/repo", GlIssueIid: 12,
	})

	// Close.
	closePayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 12, "title": "Close me", "description": "", "action": "close"},
		"project": {"id": 101, "path_with_namespace": "testorg-issue-close/repo", "namespace": "testorg-issue-close"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	closeReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(closePayload))
	closeReq.Header.Set("X-Gitlab-Token", "s")
	closeReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), closeReq)

	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if issue.Status != "done" {
		t.Errorf("status: got %q, want %q", issue.Status, "done")
	}
}

// TestHandleGitLabIssueEvent_Reopen tests that action=reopen marks the issue In Progress.
func TestHandleGitLabIssueEvent_Reopen(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-reopen",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Seed + close.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	for _, p := range []string{
		`{"object_kind":"issue","object_attributes":{"iid":13,"title":"Reopen me","description":"","action":"open"},"project":{"id":102,"path_with_namespace":"testorg-issue-reopen/repo","namespace":"testorg-issue-reopen"},"labels":[{"title":"agent"}],"assignees":[]}`,
		`{"object_kind":"issue","object_attributes":{"iid":13,"title":"Reopen me","description":"","action":"close"},"project":{"id":102,"path_with_namespace":"testorg-issue-reopen/repo","namespace":"testorg-issue-reopen"},"labels":[{"title":"agent"}],"assignees":[]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(p))
		req.Header.Set("X-Gitlab-Token", "s")
		req.Header.Set("X-Gitlab-Event", "Issue Hook")
		testHandler.HandleGitLabWebhook(httptest.NewRecorder(), req)
	}

	row, _ := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-reopen/repo", GlIssueIid: 13,
	})

	// Reopen.
	reopenPayload := `{"object_kind":"issue","object_attributes":{"iid":13,"title":"Reopen me","description":"","action":"reopen"},"project":{"id":102,"path_with_namespace":"testorg-issue-reopen/repo","namespace":"testorg-issue-reopen"},"labels":[{"title":"agent"}],"assignees":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(reopenPayload))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), req)

	issue, _ := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if issue.Status != "in_progress" {
		t.Errorf("status: got %q, want %q", issue.Status, "in_progress")
	}
}

// TestHandleGitLabNoteEvent_CreatesComment tests that a Note Hook creates a
// Multica comment authored by a persona agent matching the GitLab user.
func TestHandleGitLabNoteEvent_CreatesComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-note-create",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
		// Clean persona agent created by the note path.
		if agent, err := testHandler.Queries.GetAgentBySystemKey(ctx, db.GetAgentBySystemKeyParams{
			WorkspaceID: wsUUID,
			SystemKey:   pgtype.Text{String: "gitlab:4242", Valid: true},
		}); err == nil {
			_, _ = testPool.Exec(ctx, `DELETE FROM agent_invocation_target WHERE agent_id = $1`, agent.ID)
			_, _ = testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agent.ID)
		}
	})

	// Create an issue via Issue Hook first.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":20,"title":"Note target","description":"","action":"open"},"project":{"id":200,"path_with_namespace":"testorg-note-create/repo","namespace":"testorg-note-create"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	// Fire Note Hook with full GitLab user identity.
	notePayload := `{
		"object_kind": "note",
		"object_attributes": {
			"noteable_type": "Issue",
			"system": false,
			"id": 777,
			"note": "Hello from GitLab"
		},
		"project": {"path_with_namespace": "testorg-note-create/repo", "namespace": "testorg-note-create"},
		"issue": {"iid": 20},
		"user": {
			"id": 4242,
			"username": "gitlabuser",
			"name": "Git Lab User",
			"avatar_url": "https://gitlab.example.com/uploads/user/avatar/4242/avatar.png"
		}
	}`
	noteReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
	noteReq.Header.Set("X-Gitlab-Token", "s")
	noteReq.Header.Set("X-Gitlab-Event", "Note Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), noteReq)

	// Verify comment exists with gitlab_note_id set.
	comment, err := testHandler.Queries.GetCommentByGitLabNoteID(ctx, pgtype.Int8{Int64: 777, Valid: true})
	if err != nil {
		t.Fatalf("comment with gitlab_note_id not found: %v", err)
	}
	if comment.Content != "Hello from GitLab" {
		t.Errorf("comment body: got %q, want bare note body", comment.Content)
	}
	if comment.AuthorType != "agent" {
		t.Fatalf("author_type: got %q, want agent", comment.AuthorType)
	}

	agent, err := testHandler.Queries.GetAgent(ctx, comment.AuthorID)
	if err != nil {
		t.Fatalf("persona agent not found: %v", err)
	}
	if agent.Name != "Git Lab User" {
		t.Errorf("persona name: got %q, want %q", agent.Name, "Git Lab User")
	}
	if !agent.AvatarUrl.Valid || agent.AvatarUrl.String != "https://gitlab.example.com/uploads/user/avatar/4242/avatar.png" {
		t.Errorf("persona avatar: got %v", agent.AvatarUrl)
	}
	if !agent.SystemKey.Valid || agent.SystemKey.String != "gitlab:4242" {
		t.Errorf("persona system_key: got %v", agent.SystemKey)
	}
	if agent.MaxConcurrentTasks != 0 {
		t.Errorf("persona max_concurrent_tasks: got %d, want 0", agent.MaxConcurrentTasks)
	}
	if agent.Kind != "system" {
		t.Errorf("persona kind: got %q, want system", agent.Kind)
	}

	// Second note from the same GitLab user reuses the persona agent.
	note2 := `{
		"object_kind": "note",
		"object_attributes": {
			"noteable_type": "Issue",
			"system": false,
			"id": 778,
			"note": "Second note"
		},
		"project": {"path_with_namespace": "testorg-note-create/repo", "namespace": "testorg-note-create"},
		"issue": {"iid": 20},
		"user": {
			"id": 4242,
			"username": "gitlabuser",
			"name": "Git Lab User",
			"avatar_url": "https://gitlab.example.com/uploads/user/avatar/4242/avatar.png"
		}
	}`
	note2Req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(note2))
	note2Req.Header.Set("X-Gitlab-Token", "s")
	note2Req.Header.Set("X-Gitlab-Event", "Note Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), note2Req)

	comment2, err := testHandler.Queries.GetCommentByGitLabNoteID(ctx, pgtype.Int8{Int64: 778, Valid: true})
	if err != nil {
		t.Fatalf("second comment not found: %v", err)
	}
	if comment2.AuthorID != comment.AuthorID {
		t.Errorf("expected same persona agent, got %s vs %s",
			uuidToString(comment2.AuthorID), uuidToString(comment.AuthorID))
	}
}

// TestHandleGitLabNoteEvent_DuplicateSkipped tests that a Note Hook with an
// already-seen gitlab_note_id is silently skipped (echo loop prevention).
func TestHandleGitLabNoteEvent_DuplicateSkipped(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-note-dup",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create issue.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":21,"title":"Dup note target","description":"","action":"open"},"project":{"id":201,"path_with_namespace":"testorg-note-dup/repo","namespace":"testorg-note-dup"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	notePayload := `{"object_kind":"note","object_attributes":{"noteable_type":"Issue","system":false,"id":888,"note":"Once"},"project":{"path_with_namespace":"testorg-note-dup/repo","namespace":"testorg-note-dup"},"issue":{"iid":21},"user":{"username":"u"}}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
		req.Header.Set("X-Gitlab-Token", "s")
		req.Header.Set("X-Gitlab-Event", "Note Hook")
		testHandler.HandleGitLabWebhook(httptest.NewRecorder(), req)
	}

	// Only one comment should exist for note_id 888.
	// The unique index enforces this; verify via direct query.
	var count int
	err = testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE gitlab_note_id = 888`).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 comment for note 888, got %d", count)
	}
}

// TestHandleGitLabNoteEvent_SentinelSkipped verifies Note Hook ignores notes
// marked with the Multica dual-write sentinel (appended by Multica code, not
// free-form model text).
func TestHandleGitLabNoteEvent_SentinelSkipped(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-sentinel",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":31,"title":"Sentinel","description":"","action":"open"},"project":{"id":301,"path_with_namespace":"testorg-sentinel/repo","namespace":"testorg-sentinel"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-sentinel/repo", GlIssueIid: 31,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not found: %v", err)
	}

	noteBody := AppendGitLabNoteRelaySentinel("Posted via Multica-controlled dual-write")
	notePayload := fmt.Sprintf(`{"object_kind":"note","object_attributes":{"noteable_type":"Issue","system":false,"id":9001,"note":%q},"project":{"path_with_namespace":"testorg-sentinel/repo","namespace":"testorg-sentinel"},"issue":{"iid":31},"user":{"username":"bot"}}`, noteBody)
	noteReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
	noteReq.Header.Set("X-Gitlab-Token", "s")
	noteReq.Header.Set("X-Gitlab-Event", "Note Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), noteReq)

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, row.IssueID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("sentinel note should not create a Multica comment, got %d", count)
	}
}

// TestHandleGitLabNoteEvent_DualWriteLinksExistingComment verifies that when
// Multica already has a comment with the same body (agent dual-write), the
// Note Hook attaches gitlab_note_id instead of creating a second comment.
func TestHandleGitLabNoteEvent_DualWriteLinksExistingComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-dualwrite",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":32,"title":"Dual write","description":"","action":"open"},"project":{"id":302,"path_with_namespace":"testorg-dualwrite/repo","namespace":"testorg-dualwrite"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-dualwrite/repo", GlIssueIid: 32,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not found: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	const body = "Agent progress update from dual-write"
	comment, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    userUUID,
		Content:     body,
		Type:        "comment",
	})
	if err != nil {
		t.Fatalf("create multica comment: %v", err)
	}

	notePayload := fmt.Sprintf(`{"object_kind":"note","object_attributes":{"noteable_type":"Issue","system":false,"id":9002,"note":%q},"project":{"path_with_namespace":"testorg-dualwrite/repo","namespace":"testorg-dualwrite"},"issue":{"iid":32},"user":{"id":99,"username":"agentbot","name":"Agent Bot"}}`, body)
	noteReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
	noteReq.Header.Set("X-Gitlab-Token", "s")
	noteReq.Header.Set("X-Gitlab-Event", "Note Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), noteReq)

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("dual-write should keep a single Multica comment, got %d", count)
	}
	linked, err := testHandler.Queries.GetCommentByGitLabNoteID(ctx, pgtype.Int8{Int64: 9002, Valid: true})
	if err != nil {
		t.Fatalf("expected gitlab_note_id linked on existing comment: %v", err)
	}
	if linked.ID != comment.ID {
		t.Errorf("linked wrong comment: got %s want %s", uuidToString(linked.ID), uuidToString(comment.ID))
	}
}

func TestAppendGitLabNoteRelaySentinel(t *testing.T) {
	got := AppendGitLabNoteRelaySentinel("hello")
	if !strings.Contains(got, gitlabNoteRelaySentinel) {
		t.Fatalf("missing sentinel: %q", got)
	}
	// Idempotent.
	if again := AppendGitLabNoteRelaySentinel(got); again != got {
		t.Fatalf("second append changed body:\n%s\nvs\n%s", again, got)
	}
}

// TestGetGitLabIssueForIssue tests the GET /api/issues/:id/gitlab-issue endpoint.
func TestGetGitLabIssueForIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-get-issue",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create issue via webhook.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":40,"title":"Get me","description":"","action":"open"},"project":{"id":400,"path_with_namespace":"testorg-get-issue/repo","namespace":"testorg-get-issue"},"labels":[{"title":"agent"}],"assignees":[{"username":"getuser"}]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-get-issue/repo", GlIssueIid: 40,
	})
	if err != nil {
		t.Fatalf("seed issue not found: %v", err)
	}
	issueIDStr := uuidToString(row.IssueID)

	// GET /api/issues/:id/gitlab-issue
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueIDStr)
	req := httptest.NewRequest(http.MethodGet, "/api/issues/"+issueIDStr+"/gitlab-issue", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.GetGitLabIssueForIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		GlIssueIID         int    `json:"gl_issue_iid"`
		ProjectPath        string `json:"project_path"`
		URL                string `json:"url"`
		GlAssigneeUsername string `json:"gl_assignee_username"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.GlIssueIID != 40 {
		t.Errorf("gl_issue_iid: got %d, want 40", resp.GlIssueIID)
	}
	if resp.ProjectPath != "testorg-get-issue/repo" {
		t.Errorf("project_path: got %q", resp.ProjectPath)
	}
	if resp.GlAssigneeUsername != "getuser" {
		t.Errorf("gl_assignee_username: got %q, want %q", resp.GlAssigneeUsername, "getuser")
	}
	if !strings.Contains(resp.URL, "testorg-get-issue/repo") {
		t.Errorf("url missing project path: %q", resp.URL)
	}
}

// TestGetGitLabIssueForIssue_NotFound tests that a non-linked issue returns 404.
func TestGetGitLabIssueForIssue_NotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}

	// Use a UUID that doesn't have a gitlab_issue link.
	randomUUID := pgtype.UUID{}
	if _, err := rand.Read(randomUUID.Bytes[:]); err != nil {
		t.Fatalf("generate random uuid: %v", err)
	}
	randomUUID.Valid = true

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidToString(randomUUID))
	req := httptest.NewRequest(http.MethodGet, "/api/issues/"+uuidToString(randomUUID)+"/gitlab-issue", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.GetGitLabIssueForIssue(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestListGitLabConnections_WebhookSecretRoleGating verifies that the
// per-connection webhook_secret is returned only to owners/admins and omitted
// for plain members. Also covers lazy issuance for legacy empty-secret rows.
func TestListGitLabConnections_WebhookSecretRoleGating(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)

	const secret = "glwh_test-per-connection-secret"
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "secret-gating-ns",
		NamespaceType: "group",
		AccessToken:   "dummy",
		WebhookSecret: secret,
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Env secret must NOT leak into the list response.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "env-secret-must-not-appear")
	t.Setenv("GITLAB_URL", "https://gitlab.example.com")
	t.Setenv("GITLAB_APP_ID", "app-id")
	t.Setenv("GITLAB_APP_SECRET", "app-secret")
	t.Setenv("GITLAB_SECRET_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	call := func(t *testing.T, role string) ListGitLabConnectionsResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/gitlab/connections", nil)
		req = withURLParam(req, "id", testWorkspaceID)
		req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: role}))
		w := httptest.NewRecorder()
		testHandler.ListGitLabConnections(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListGitLabConnections(%s): %d %s", role, w.Code, w.Body.String())
		}
		var body ListGitLabConnectionsResponse
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode body (%s): %v", role, err)
		}
		return body
	}

	findConn := func(body ListGitLabConnectionsResponse) *GitLabConnectionResponse {
		for i := range body.Connections {
			if body.Connections[i].ID == uuidToString(conn.ID) {
				return &body.Connections[i]
			}
		}
		return nil
	}

	t.Run("admin sees per-connection webhook_secret", func(t *testing.T) {
		body := call(t, "admin")
		if !body.CanManage {
			t.Errorf("can_manage = false, want true")
		}
		row := findConn(body)
		if row == nil {
			t.Fatal("connection missing from admin response")
		}
		if row.WebhookSecret == nil || *row.WebhookSecret != secret {
			t.Errorf("webhook_secret = %v, want %q", row.WebhookSecret, secret)
		}
	})

	t.Run("owner sees per-connection webhook_secret", func(t *testing.T) {
		body := call(t, "owner")
		row := findConn(body)
		if row == nil {
			t.Fatal("connection missing from owner response")
		}
		if row.WebhookSecret == nil || *row.WebhookSecret != secret {
			t.Errorf("webhook_secret = %v, want %q", row.WebhookSecret, secret)
		}
	})

	t.Run("member does not see webhook_secret", func(t *testing.T) {
		body := call(t, "member")
		if body.CanManage {
			t.Errorf("can_manage = true, want false for non-admin member")
		}
		row := findConn(body)
		if row == nil {
			t.Fatal("member should still see connection rows")
		}
		if row.WebhookSecret != nil {
			t.Errorf("webhook_secret must be omitted for non-admin members, got %q", *row.WebhookSecret)
		}
	})

	t.Run("guest does not see webhook_secret", func(t *testing.T) {
		body := call(t, "guest")
		row := findConn(body)
		if row == nil {
			t.Fatal("guest should still see connection rows")
		}
		if row.WebhookSecret != nil {
			t.Errorf("webhook_secret must be omitted for guest, got %q", *row.WebhookSecret)
		}
	})
}

// TestRotateGitLabConnectionWebhookSecret issues a new secret and invalidates the old one.
func TestRotateGitLabConnectionWebhookSecret(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	const oldSecret = "glwh_old-secret-value-for-rotate"
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "rotate-secret-ns",
		NamespaceType: "group",
		AccessToken:   "dummy",
		WebhookSecret: oldSecret,
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	req := httptest.NewRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/gitlab/connections/"+uuidToString(conn.ID)+"/rotate-webhook-secret",
		nil)
	req = withURLParams(req, "id", testWorkspaceID, "connectionId", uuidToString(conn.ID))
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: "admin"}))
	w := httptest.NewRecorder()
	testHandler.RotateGitLabConnectionWebhookSecret(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Rotate: expected 200, got %d %s", w.Code, w.Body.String())
	}
	var body GitLabConnectionResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.WebhookSecret == nil || *body.WebhookSecret == "" {
		t.Fatal("expected new webhook_secret in response")
	}
	if *body.WebhookSecret == oldSecret {
		t.Fatal("rotated secret must differ from the previous value")
	}
	if !strings.HasPrefix(*body.WebhookSecret, "glwh_") {
		t.Errorf("secret %q missing glwh_ prefix", *body.WebhookSecret)
	}

	// Old secret rejected; new secret accepted.
	payload := `{"project":{"namespace":"rotate-secret-ns"}}`
	oldReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload))
	oldReq.Header.Set("X-Gitlab-Token", oldSecret)
	oldReq.Header.Set("X-Gitlab-Event", "Push Hook")
	oldW := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(oldW, oldReq)
	if oldW.Code != http.StatusUnauthorized {
		t.Fatalf("old secret: expected 401, got %d", oldW.Code)
	}

	newReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload))
	newReq.Header.Set("X-Gitlab-Token", *body.WebhookSecret)
	newReq.Header.Set("X-Gitlab-Event", "Push Hook")
	newW := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(newW, newReq)
	if newW.Code != http.StatusNoContent {
		t.Fatalf("new secret: expected 204, got %d", newW.Code)
	}
}

// TestHandleGitLabWebhook_PerConnectionSecretIsolated ensures workspace A's secret
// cannot authenticate events for workspace B's namespace.
func TestHandleGitLabWebhook_PerConnectionSecretIsolated(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "isolated-ns",
		NamespaceType: "group",
		AccessToken:   "dummy",
		WebhookSecret: "glwh_correct-for-isolated",
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})
	t.Setenv("GITLAB_WEBHOOK_SECRET", "glwh_other-workspace-env")

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(
		`{"project":{"namespace":"isolated-ns"}}`,
	))
	req.Header.Set("X-Gitlab-Token", "glwh_other-workspace-env")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("env/other secret must not authenticate a connection with its own secret; got %d", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(
		`{"project":{"namespace":"isolated-ns"}}`,
	))
	req2.Header.Set("X-Gitlab-Token", "glwh_correct-for-isolated")
	req2.Header.Set("X-Gitlab-Event", "Push Hook")
	w2 := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("correct connection secret: expected 204, got %d", w2.Code)
	}
}
