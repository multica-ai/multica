package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestSupportDispatchHistoricalReadbackRouteDefaultOffAndAuthenticated(t *testing.T) {
	router, h := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{})
	path := "/api/workspaces/" + testWorkspaceID + "/support-dispatch/claims/" + uuid.NewString()
	request := func(token string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return req
	}
	check := func(req *http.Request, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("route status %d, want %d: %s", w.Code, want, w.Body.String())
		}
	}
	check(request(""), http.StatusUnauthorized)
	check(request(testToken), http.StatusNotFound) // Feature remains off in production wiring.
	if h.SupportDispatchClaimEnabled {
		t.Fatal("support dispatch enabled without an operator flag")
	}
	t.Setenv("FF_SUPPORT_DISPATCH_CLAIM", "true")
	flags, err := featureflag.NewServiceFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	router, h = NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil,
		RouterOptions{FeatureFlags: flags})
	if !h.SupportDispatchClaimEnabled {
		t.Fatal("operator flag did not enable support dispatch")
	}
	check(request(testToken), http.StatusNotFound) // Authenticated owner; no claim exists.
	malformed := request(testToken)
	malformed.URL.Path = "/api/workspaces/" + testWorkspaceID + "/support-dispatch/claims/bad-uuid"
	check(malformed, http.StatusBadRequest) // Reaches the historical handler, not a missing route.
}

func TestSupportDispatchHistoricalReadbackAfterDeleteThroughRouter(t *testing.T) {
	ctx := context.Background()
	var issueID string
	err := testPool.QueryRow(ctx, `INSERT INTO issue
		(workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		VALUES ($1, 'Historical claim route', 'todo', 'none', 'member', $2, 'member', $2)
		RETURNING id`, testWorkspaceID, testUserID).Scan(&issueID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM support_dispatch_claim WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})
	const content = "reviewed support attestation"
	var commentID string
	if err := testPool.QueryRow(ctx, `INSERT INTO comment
		(issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, $4, 'comment') RETURNING id`,
		issueID, testWorkspaceID, testUserID, content).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := testPool.QueryRow(ctx, `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FF_SUPPORT_DISPATCH_CLAIM", "true")
	flags, err := featureflag.NewServiceFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	router, h := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil,
		RouterOptions{FeatureFlags: flags})
	if !h.SupportDispatchClaimEnabled {
		t.Fatal("operator flag did not enable support dispatch")
	}
	request := func(method, path string, body any) *http.Request {
		t.Helper()
		var data []byte
		if body != nil {
			var err error
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		return req
	}
	check := func(req *http.Request, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, want %d: %s", req.Method, req.URL.Path, w.Code, want, w.Body.String())
		}
		return w
	}
	digest := sha256.Sum256([]byte(content))
	claimPath := "/api/issues/" + issueID + "/support-dispatch/claim"
	check(request(http.MethodPut, claimPath, map[string]any{
		"expected_revision": revision, "staff_comment_id": commentID,
		"staff_comment_revision": 1, "attestation_digest": hex.EncodeToString(digest[:]),
	}), http.StatusCreated)
	check(request(http.MethodDelete, "/api/issues/"+issueID, nil), http.StatusNoContent)
	var claim struct {
		WorkspaceID       string `json:"workspace_id"`
		IssueID           string `json:"issue_id"`
		StaffCommentID    string `json:"staff_comment_id"`
		AttestationDigest string `json:"attestation_digest"`
	}
	readback := "/api/workspaces/" + testWorkspaceID + "/support-dispatch/claims/" + issueID
	w := check(request(http.MethodGet, readback, nil), http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &claim); err != nil {
		t.Fatal(err)
	}
	if claim.WorkspaceID != testWorkspaceID || claim.IssueID != issueID ||
		claim.StaffCommentID != commentID || claim.AttestationDigest != hex.EncodeToString(digest[:]) {
		t.Fatalf("historical claim changed after deletion: %#v", claim)
	}
	check(request(http.MethodPut, claimPath, map[string]any{
		"expected_revision": revision, "staff_comment_id": commentID,
		"staff_comment_revision": 1, "attestation_digest": hex.EncodeToString(digest[:]),
	}), http.StatusNotFound) // Never grant a second dispatch after deleting the issue.
}
