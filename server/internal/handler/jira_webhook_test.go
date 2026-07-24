package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/integrations/jira"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// mockJiraClient satisfies jira.Client without any network access; tests
// must never contact a real Jira site.
type mockJiraClient struct {
	account jira.Account
	issues  map[string]jira.Issue
	err     error
	calls   int
}

func (m *mockJiraClient) ValidateCredentials(_ context.Context, _, _, _ string) (jira.Account, error) {
	m.calls++
	return m.account, m.err
}

func (m *mockJiraClient) GetIssue(_ context.Context, _, _, _, key string) (jira.Issue, error) {
	m.calls++
	if m.err != nil {
		return jira.Issue{}, m.err
	}
	if issue, ok := m.issues[key]; ok {
		return issue, nil
	}
	return jira.Issue{}, jira.ErrIssueNotFound
}

func withJiraBox(t *testing.T) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte("j"), 32))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	prev := testHandler.JiraSecretBox
	prevClient := testHandler.JiraClient
	testHandler.JiraSecretBox = box
	t.Cleanup(func() {
		testHandler.JiraSecretBox = prev
		testHandler.JiraClient = prevClient
	})
	return box
}

const jiraTestSecret = "jira-webhook-secret"

func seedJiraConnection(t *testing.T, ctx context.Context, box *secretbox.Box, baseURL string) db.JiraConnection {
	t.Helper()
	sealedSecret, err := box.Seal([]byte(jiraTestSecret))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	sealedToken, _ := box.Seal([]byte("api-token"))
	conn, err := testHandler.Queries.UpsertJiraConnection(ctx, db.UpsertJiraConnectionParams{
		WorkspaceID:            parseUUID(testWorkspaceID),
		BaseUrl:                baseURL,
		AccountEmail:           "ops@acme.test",
		ApiTokenEncrypted:      base64.StdEncoding.EncodeToString(sealedToken),
		WebhookSecretEncrypted: base64.StdEncoding.EncodeToString(sealedSecret),
		ConnectedByID:          parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("UpsertJiraConnection: %v", err)
	}
	return conn
}

func cleanupJira(ctx context.Context) {
	testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id IN (SELECT multica_issue_id FROM jira_issue_link WHERE workspace_id = $1)`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM issue WHERE id IN (SELECT multica_issue_id FROM jira_issue_link WHERE workspace_id = $1)`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM jira_issue_link WHERE workspace_id = $1`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM jira_connection WHERE workspace_id = $1`, testWorkspaceID)
}

func jiraWebhookReq(connID string, secret string, raw []byte) *http.Request {
	req := httptest.NewRequest("POST", "/api/webhooks/jira/"+connID, bytes.NewReader(raw))
	if secret != "" {
		req.Header.Set(jira.SecretHeader, secret)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("connectionId", connID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func jiraIssuePayload(event, id, key, summary, description string) []byte {
	fields := map[string]any{
		"summary":  summary,
		"status":   map[string]any{"name": "To Do"},
		"priority": map[string]any{"name": "High"},
	}
	if description != "" {
		fields["description"] = description
	}
	raw, _ := json.Marshal(map[string]any{
		"webhookEvent": event,
		"issue":        map[string]any{"id": id, "key": key, "fields": fields},
	})
	return raw
}

func TestJiraWebhook_CreatesIssueAndLink(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	testHandler.JiraClient = &mockJiraClient{}
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	raw := jiraIssuePayload("jira:issue_created", "10042", "PROJ-7", "Fix the login flow", "the details")
	w := httptest.NewRecorder()
	testHandler.HandleJiraWebhook(w, jiraWebhookReq(uuidToString(conn.ID), jiraTestSecret, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	link, err := testHandler.Queries.GetJiraIssueLink(ctx, db.GetJiraIssueLinkParams{
		ConnectionID: conn.ID,
		JiraIssueKey: "PROJ-7",
	})
	if err != nil {
		t.Fatalf("GetJiraIssueLink: %v", err)
	}
	if link.JiraIssueID != "10042" || link.SyncStatus != "synced" {
		t.Errorf("unexpected link: %+v", link)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, link.MulticaIssueID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Title != "Fix the login flow" || issue.Description.String != "the details" {
		t.Errorf("unexpected issue: title=%q description=%q", issue.Title, issue.Description.String)
	}
	if issue.Status != "todo" || issue.OriginType.String != "jira" || uuidToString(issue.OriginID) != uuidToString(conn.ID) {
		t.Errorf("unexpected issue metadata: status=%q origin=%q/%q", issue.Status, issue.OriginType.String, uuidToString(issue.OriginID))
	}
}

func TestJiraWebhook_UpdateSyncsExistingIssue(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	testHandler.JiraClient = &mockJiraClient{}
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	fire := func(raw []byte) {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.HandleJiraWebhook(w, jiraWebhookReq(uuidToString(conn.ID), jiraTestSecret, raw))
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
		}
	}

	fire(jiraIssuePayload("jira:issue_created", "10042", "PROJ-7", "Original title", "original"))
	fire(jiraIssuePayload("jira:issue_updated", "10042", "PROJ-7", "Renamed title", "updated details"))

	link, err := testHandler.Queries.GetJiraIssueLink(ctx, db.GetJiraIssueLinkParams{
		ConnectionID: conn.ID,
		JiraIssueKey: "PROJ-7",
	})
	if err != nil {
		t.Fatalf("GetJiraIssueLink: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, link.MulticaIssueID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Title != "Renamed title" || issue.Description.String != "updated details" {
		t.Errorf("issue not synced: title=%q description=%q", issue.Title, issue.Description.String)
	}
	// Exactly one Multica issue per Jira key — the update must not create a
	// second one.
	var count int
	testPool.QueryRow(ctx, `SELECT count(*) FROM jira_issue_link WHERE workspace_id = $1`, testWorkspaceID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 link, got %d", count)
	}
}

// A webhook delivery whose body carries no fields (some Jira webhook configs
// exclude them) must be enriched via the REST client before mirroring.
func TestJiraWebhook_ThinPayloadEnrichedViaClient(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	mock := &mockJiraClient{issues: map[string]jira.Issue{
		"PROJ-9": {ID: "10099", Key: "PROJ-9", Summary: "Enriched summary", Description: "enriched details"},
	}}
	testHandler.JiraClient = mock
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	raw, _ := json.Marshal(map[string]any{
		"webhookEvent": "jira:issue_created",
		"issue":        map[string]any{"id": "10099", "key": "PROJ-9"},
	})
	w := httptest.NewRecorder()
	testHandler.HandleJiraWebhook(w, jiraWebhookReq(uuidToString(conn.ID), jiraTestSecret, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}
	if mock.calls == 0 {
		t.Error("thin payload should have triggered a client enrichment call")
	}

	link, err := testHandler.Queries.GetJiraIssueLink(ctx, db.GetJiraIssueLinkParams{
		ConnectionID: conn.ID,
		JiraIssueKey: "PROJ-9",
	})
	if err != nil {
		t.Fatalf("GetJiraIssueLink: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, link.MulticaIssueID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Title != "Enriched summary" || issue.Description.String != "enriched details" {
		t.Errorf("enrichment not applied: title=%q description=%q", issue.Title, issue.Description.String)
	}
}

func TestJiraWebhook_BadSecret(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	testHandler.JiraClient = &mockJiraClient{}
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	raw := jiraIssuePayload("jira:issue_created", "1", "PROJ-1", "x", "")
	w := httptest.NewRecorder()
	testHandler.HandleJiraWebhook(w, jiraWebhookReq(uuidToString(conn.ID), "wrong-secret", raw))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	testHandler.HandleJiraWebhook(w, jiraWebhookReq(uuidToString(conn.ID), "", raw))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing secret: expected 401, got %d", w.Code)
	}
	var count int
	testPool.QueryRow(ctx, `SELECT count(*) FROM jira_issue_link WHERE workspace_id = $1`, testWorkspaceID).Scan(&count)
	if count != 0 {
		t.Errorf("unauthenticated delivery must not create links, got %d", count)
	}
}

func TestJiraWebhook_UnknownConnection(t *testing.T) {
	withJiraBox(t)
	w := httptest.NewRecorder()
	testHandler.HandleJiraWebhook(w, jiraWebhookReq("00000000-0000-0000-0000-000000000000", jiraTestSecret, []byte(`{}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestJiraWebhook_UnconfiguredReturns503(t *testing.T) {
	prev := testHandler.JiraSecretBox
	testHandler.JiraSecretBox = nil
	t.Cleanup(func() { testHandler.JiraSecretBox = prev })

	w := httptest.NewRecorder()
	testHandler.HandleJiraWebhook(w, jiraWebhookReq("00000000-0000-0000-0000-000000000000", jiraTestSecret, []byte(`{}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestJiraWebhook_UnmodelledEventAcknowledged(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	testHandler.JiraClient = &mockJiraClient{}
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	raw, _ := json.Marshal(map[string]any{"webhookEvent": "comment_created", "issue": map[string]any{"key": "PROJ-1"}})
	w := httptest.NewRecorder()
	testHandler.HandleJiraWebhook(w, jiraWebhookReq(uuidToString(conn.ID), jiraTestSecret, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}
	var count int
	testPool.QueryRow(ctx, `SELECT count(*) FROM jira_issue_link WHERE workspace_id = $1`, testWorkspaceID).Scan(&count)
	if count != 0 {
		t.Errorf("unmodelled event must not create links, got %d", count)
	}
}

func TestJiraWebhook_MalformedTolerated(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	testHandler.JiraClient = &mockJiraClient{}
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	w := httptest.NewRecorder()
	testHandler.HandleJiraWebhook(w, jiraWebhookReq(uuidToString(conn.ID), jiraTestSecret, []byte(`{"webhookEvent":`)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}
}

// DeleteJiraConnection must sweep the connection's link rows atomically but
// keep the mirrored Multica issues, and be a complete no-op for a mismatched
// workspace (tenant guard).
func TestDeleteJiraConnection_SweepsLinksKeepsIssues(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	testHandler.JiraClient = &mockJiraClient{}
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	raw := jiraIssuePayload("jira:issue_created", "10042", "PROJ-7", "Survivor", "")
	w := httptest.NewRecorder()
	testHandler.HandleJiraWebhook(w, jiraWebhookReq(uuidToString(conn.ID), jiraTestSecret, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("seed: expected 202, got %d", w.Code)
	}
	link, err := testHandler.Queries.GetJiraIssueLink(ctx, db.GetJiraIssueLinkParams{
		ConnectionID: conn.ID, JiraIssueKey: "PROJ-7",
	})
	if err != nil {
		t.Fatalf("GetJiraIssueLink: %v", err)
	}
	issueID := link.MulticaIssueID
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, uuidToString(issueID))
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, uuidToString(issueID))
	})

	// Mismatched workspace → no-op.
	wrongWS := parseUUID("11111111-1111-1111-1111-111111111111")
	if err := testHandler.Queries.DeleteJiraConnection(ctx, db.DeleteJiraConnectionParams{
		ID: conn.ID, WorkspaceID: wrongWS,
	}); err != nil {
		t.Fatalf("DeleteJiraConnection(wrong ws): %v", err)
	}
	if _, err := testHandler.Queries.GetJiraConnectionByID(ctx, conn.ID); err != nil {
		t.Fatalf("connection must survive mismatched-workspace delete: %v", err)
	}

	// Correct workspace → connection and links gone, issue kept.
	if err := testHandler.Queries.DeleteJiraConnection(ctx, db.DeleteJiraConnectionParams{
		ID: conn.ID, WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("DeleteJiraConnection: %v", err)
	}
	var linkCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM jira_issue_link WHERE connection_id = $1`, uuidToString(conn.ID)).Scan(&linkCount)
	if linkCount != 0 {
		t.Errorf("links must be swept with the connection, got %d", linkCount)
	}
	if _, err := testHandler.Queries.GetIssue(ctx, issueID); err != nil {
		t.Errorf("mirrored issue must be kept after disconnect: %v", err)
	}
}

// Compile-time check: the mock must satisfy the production seam.
var _ jira.Client = (*mockJiraClient)(nil)
