package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIOSShareTokenIsRandomAndHashed(t *testing.T) {
	a, err := generateIOSShareToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateIOSShareToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !strings.HasPrefix(a, iosShareTokenPrefix) {
		t.Fatalf("tokens must be distinct and prefixed: %q %q", a, b)
	}
	if hashIOSShareToken(a) == a || hashIOSShareToken(a) == hashIOSShareToken(b) {
		t.Fatal("token hashes must conceal and distinguish credentials")
	}
}

func TestIOSShareInboxCreatesProjectBoundIssueAndRevocationStopsIt(t *testing.T) {
	ctx := context.Background()
	var projectID string
	if err := testPool.QueryRow(ctx, `INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id::text`, testWorkspaceID, "iOS Share Test").Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM project WHERE id=$1`, projectID) })

	createW := httptest.NewRecorder()
	createReq := newRequest(http.MethodPost, "/api/cerebro/share-inboxes", map[string]any{"project_id": projectID})
	testHandler.CreateIOSShareInbox(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create inbox: %d %s", createW.Code, createW.Body.String())
	}
	var inbox iosShareInboxResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &inbox); err != nil {
		t.Fatal(err)
	}
	if inbox.Token == "" {
		t.Fatal("raw token must be returned once at creation")
	}

	shareW := httptest.NewRecorder()
	shareReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/ios-share/"+inbox.Token, strings.NewReader(`{"text":"A useful page","url":"https://example.com/article"}`))
	shareReq = withURLParam(shareReq, "token", inbox.Token)
	testHandler.HandleIOSShareInbox(shareW, shareReq)
	if shareW.Code != http.StatusCreated {
		t.Fatalf("share: %d %s", shareW.Code, shareW.Body.String())
	}
	var created struct {
		IssueID string `json:"issue_id"`
	}
	if err := json.Unmarshal(shareW.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id=$1`, created.IssueID) })
	var gotProject, gotCreator, gotDescription string
	if err := testPool.QueryRow(ctx, `SELECT project_id::text, creator_id::text, description FROM issue WHERE id=$1`, created.IssueID).Scan(&gotProject, &gotCreator, &gotDescription); err != nil {
		t.Fatal(err)
	}
	if gotProject != projectID || gotCreator != testUserID || !strings.Contains(gotDescription, "https://example.com/article") {
		t.Fatalf("issue binding mismatch: project=%s creator=%s description=%q", gotProject, gotCreator, gotDescription)
	}

	revokeW := httptest.NewRecorder()
	revokeReq := newRequest(http.MethodDelete, "/api/cerebro/share-inboxes/"+inbox.ID, nil)
	revokeReq = withURLParam(revokeReq, "id", inbox.ID)
	testHandler.RevokeIOSShareInbox(revokeW, revokeReq)
	if revokeW.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", revokeW.Code, revokeW.Body.String())
	}

	deniedW := httptest.NewRecorder()
	deniedReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/ios-share/"+inbox.Token, strings.NewReader(`{"text":"must not create"}`))
	deniedReq = withURLParam(deniedReq, "token", inbox.Token)
	testHandler.HandleIOSShareInbox(deniedW, deniedReq)
	if deniedW.Code != http.StatusNotFound {
		t.Fatalf("revoked token: got %d, want 404", deniedW.Code)
	}
}

func TestIOSShareInboxRejectsUnknownFields(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/ios-share/not-a-token", strings.NewReader(`{"text":"x","project_id":"attacker-choice"}`))
	req = withURLParam(req, "token", "not-a-token")
	testHandler.HandleIOSShareInbox(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

func TestDecodeIOSShareImageRejectsNonImages(t *testing.T) {
	encoded := "aGVsbG8=" // hello
	if _, err := decodeIOSShareImage(encoded); err == nil {
		t.Fatal("plain text must not be accepted as an image attachment")
	}
}
