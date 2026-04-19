//go:build integration_live

// Package gitlab integration-live test: exercises the full GitLab REST client
// surface against a real gitlab.com project. Opt in via build tag so normal
// test runs don't hit the network.
//
// Run with:
//
//	GITLAB_TEST_URL=https://gitlab.com \
//	GITLAB_TEST_PROJECT_PATH=your-group/your-project \
//	GITLAB_TEST_PAT=<your PAT> \
//	go test -tags integration_live -v ./pkg/gitlab/
//
// Every test creates a GitLab-side resource and cleans it up.

package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type liveEnv struct {
	BaseURL     string
	Token       string
	ProjectID   int64
	ProjectPath string
}

// getLiveEnv reads GITLAB_TEST_* env vars. Skips the test if any are missing.
// Resolves the project path to a numeric ID via a direct API call (not through
// our client — we want the ID before exercising the client under test).
func getLiveEnv(t *testing.T) *liveEnv {
	t.Helper()
	baseURL := os.Getenv("GITLAB_TEST_URL")
	pat := os.Getenv("GITLAB_TEST_PAT")
	projectPath := os.Getenv("GITLAB_TEST_PROJECT_PATH")
	if baseURL == "" || pat == "" || projectPath == "" {
		t.Skip("set GITLAB_TEST_URL + GITLAB_TEST_PAT + GITLAB_TEST_PROJECT_PATH to run live tests")
	}

	encoded := url.PathEscape(projectPath)
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v4/projects/"+encoded, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("PRIVATE-TOKEN", pat)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("resolve project: status %d", resp.StatusCode)
	}
	var pj struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pj); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	return &liveEnv{
		BaseURL:     baseURL,
		Token:       pat,
		ProjectID:   pj.ID,
		ProjectPath: projectPath,
	}
}

// unique returns a collision-free suffix for GitLab-side resource names.
func unique() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func TestIntegrationLive_IssueCRUD(t *testing.T) {
	env := getLiveEnv(t)
	c := NewClient(env.BaseURL, http.DefaultClient)
	ctx := context.Background()
	suffix := unique()

	title := "Multica E2E-" + suffix
	issue, err := c.CreateIssue(ctx, env.Token, env.ProjectID, CreateIssueInput{
		Title:       title,
		Description: "created by integration_live test",
		Labels:      []string{"status::todo"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if issue.IID == 0 || issue.Title != title {
		t.Fatalf("unexpected issue: %+v", issue)
	}
	t.Logf("created issue iid=%d id=%d", issue.IID, issue.ID)

	defer func() {
		if err := c.DeleteIssue(ctx, env.Token, env.ProjectID, issue.IID); err != nil {
			t.Errorf("cleanup DeleteIssue: %v", err)
		}
	}()

	newTitle := title + " (updated)"
	closeEvent := "close"
	updated, err := c.UpdateIssue(ctx, env.Token, env.ProjectID, issue.IID, UpdateIssueInput{
		Title:        &newTitle,
		AddLabels:    []string{"status::done"},
		RemoveLabels: []string{"status::todo"},
		StateEvent:   &closeEvent,
	})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if updated.Title != newTitle {
		t.Errorf("title = %q, want %q", updated.Title, newTitle)
	}
	if updated.State != "closed" {
		t.Errorf("state = %q, want closed", updated.State)
	}
}

func TestIntegrationLive_NoteCRUD(t *testing.T) {
	env := getLiveEnv(t)
	c := NewClient(env.BaseURL, http.DefaultClient)
	ctx := context.Background()
	suffix := unique()

	parent, err := c.CreateIssue(ctx, env.Token, env.ProjectID, CreateIssueInput{
		Title: "Multica E2E-NoteCRUD-" + suffix,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	defer c.DeleteIssue(ctx, env.Token, env.ProjectID, parent.IID)

	body := "hello from integration_live " + suffix
	note, err := c.CreateNote(ctx, env.Token, env.ProjectID, parent.IID, body)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if note.Body != body {
		t.Errorf("note body = %q, want %q", note.Body, body)
	}
	t.Logf("created note id=%d", note.ID)

	editedBody := body + " — edited"
	edited, err := c.UpdateNote(ctx, env.Token, env.ProjectID, parent.IID, note.ID, editedBody)
	if err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	if edited.Body != editedBody {
		t.Errorf("edited note body = %q, want %q", edited.Body, editedBody)
	}

	if err := c.DeleteNote(ctx, env.Token, env.ProjectID, parent.IID, note.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	// Idempotent 404.
	if err := c.DeleteNote(ctx, env.Token, env.ProjectID, parent.IID, note.ID); err != nil {
		t.Errorf("DeleteNote should be idempotent on 404, got %v", err)
	}
}

func TestIntegrationLive_IssueAwardEmoji(t *testing.T) {
	env := getLiveEnv(t)
	c := NewClient(env.BaseURL, http.DefaultClient)
	ctx := context.Background()
	suffix := unique()

	parent, err := c.CreateIssue(ctx, env.Token, env.ProjectID, CreateIssueInput{
		Title: "Multica E2E-IssueAward-" + suffix,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	defer c.DeleteIssue(ctx, env.Token, env.ProjectID, parent.IID)

	award, err := c.CreateIssueAwardEmoji(ctx, env.Token, env.ProjectID, parent.IID, "thumbsup")
	if err != nil {
		t.Fatalf("CreateIssueAwardEmoji: %v", err)
	}
	if award.Name != "thumbsup" {
		t.Errorf("award name = %q, want thumbsup", award.Name)
	}
	t.Logf("awarded emoji id=%d", award.ID)

	if err := c.DeleteIssueAwardEmoji(ctx, env.Token, env.ProjectID, parent.IID, award.ID); err != nil {
		t.Fatalf("DeleteIssueAwardEmoji: %v", err)
	}
	if err := c.DeleteIssueAwardEmoji(ctx, env.Token, env.ProjectID, parent.IID, award.ID); err != nil {
		t.Errorf("DeleteIssueAwardEmoji should be idempotent on 404, got %v", err)
	}
}

func TestIntegrationLive_NoteAwardEmoji(t *testing.T) {
	env := getLiveEnv(t)
	c := NewClient(env.BaseURL, http.DefaultClient)
	ctx := context.Background()
	suffix := unique()

	parent, err := c.CreateIssue(ctx, env.Token, env.ProjectID, CreateIssueInput{
		Title: "Multica E2E-NoteAward-" + suffix,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	defer c.DeleteIssue(ctx, env.Token, env.ProjectID, parent.IID)

	note, err := c.CreateNote(ctx, env.Token, env.ProjectID, parent.IID, "react-target "+suffix)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	award, err := c.CreateNoteAwardEmoji(ctx, env.Token, env.ProjectID, parent.IID, note.ID, "heart")
	if err != nil {
		t.Fatalf("CreateNoteAwardEmoji: %v", err)
	}
	if award.Name != "heart" {
		t.Errorf("award name = %q, want heart", award.Name)
	}
	if err := c.DeleteNoteAwardEmoji(ctx, env.Token, env.ProjectID, parent.IID, note.ID, award.ID); err != nil {
		t.Fatalf("DeleteNoteAwardEmoji: %v", err)
	}
}

func TestIntegrationLive_Subscribe(t *testing.T) {
	env := getLiveEnv(t)
	c := NewClient(env.BaseURL, http.DefaultClient)
	ctx := context.Background()
	suffix := unique()

	parent, err := c.CreateIssue(ctx, env.Token, env.ProjectID, CreateIssueInput{
		Title: "Multica E2E-Subscribe-" + suffix,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	defer c.DeleteIssue(ctx, env.Token, env.ProjectID, parent.IID)

	// Authors auto-subscribe on create. Unsubscribe first, then re-subscribe.
	if err := c.Unsubscribe(ctx, env.Token, env.ProjectID, parent.IID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	// 304 idempotency.
	if err := c.Unsubscribe(ctx, env.Token, env.ProjectID, parent.IID); err != nil {
		t.Errorf("Unsubscribe should be 304-idempotent, got %v", err)
	}

	if err := c.Subscribe(ctx, env.Token, env.ProjectID, parent.IID); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := c.Subscribe(ctx, env.Token, env.ProjectID, parent.IID); err != nil {
		t.Errorf("Subscribe should be 304-idempotent, got %v", err)
	}
}

func TestIntegrationLive_ListProjectMembers(t *testing.T) {
	env := getLiveEnv(t)
	c := NewClient(env.BaseURL, http.DefaultClient)
	ctx := context.Background()

	members, err := c.ListProjectMembers(ctx, env.Token, env.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	if len(members) == 0 {
		t.Errorf("expected at least one project member, got 0")
	}
	t.Logf("got %d members", len(members))
	for _, m := range members {
		if m.ID == 0 || m.Username == "" {
			t.Errorf("malformed member: %+v", m)
		}
	}
}

func TestIntegrationLive_ListIssues(t *testing.T) {
	env := getLiveEnv(t)
	c := NewClient(env.BaseURL, http.DefaultClient)
	ctx := context.Background()
	suffix := unique()

	var created []int
	for i := 0; i < 3; i++ {
		issue, err := c.CreateIssue(ctx, env.Token, env.ProjectID, CreateIssueInput{
			Title: fmt.Sprintf("Multica E2E-List-%s-%d", suffix, i),
		})
		if err != nil {
			t.Fatalf("CreateIssue %d: %v", i, err)
		}
		created = append(created, issue.IID)
	}
	defer func() {
		for _, iid := range created {
			_ = c.DeleteIssue(ctx, env.Token, env.ProjectID, iid)
		}
	}()

	issues, err := c.ListIssues(ctx, env.Token, env.ProjectID, ListIssuesParams{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) < 3 {
		t.Errorf("ListIssues returned %d, want >= 3", len(issues))
	}

	found := 0
	for _, iss := range issues {
		if strings.Contains(iss.Title, "Multica E2E-List-"+suffix) {
			found++
		}
	}
	if found != 3 {
		t.Errorf("found %d of 3 created issues in list response", found)
	}
}

func TestIntegrationLive_WebhookRegistration(t *testing.T) {
	env := getLiveEnv(t)
	c := NewClient(env.BaseURL, http.DefaultClient)
	ctx := context.Background()
	suffix := unique()

	hookURL := "https://example.invalid/webhooks/multica/" + suffix
	secretToken := "test-secret-" + suffix

	hook, err := c.CreateProjectHook(ctx, env.Token, env.ProjectID, CreateProjectHookInput{
		URL:                   hookURL,
		Token:                 secretToken,
		IssuesEvents:          true,
		NoteEvents:            true,
		EmojiEvents:           true,
		EnableSSLVerification: true,
	})
	if err != nil {
		t.Fatalf("CreateProjectHook: %v", err)
	}
	t.Logf("created webhook id=%d", hook.ID)

	defer func() {
		if err := c.DeleteProjectHook(ctx, env.Token, env.ProjectID, hook.ID); err != nil {
			t.Errorf("cleanup DeleteProjectHook: %v", err)
		}
	}()

	if hook.URL != hookURL {
		t.Errorf("hook URL = %q, want %q", hook.URL, hookURL)
	}
}
