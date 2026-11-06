package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestGetIssueDeletedIdentifierReturnsTombstone pins the #9070 contract:
// after a hard delete the identifier must answer with a machine-readable
// tombstone (identifier, number, status=deleted, deleted_at), a never-issued
// number must keep returning not found, and the deleted issue's UUID keeps
// returning not found — the tombstone is reachable by identifier only.
func TestGetIssueDeletedIdentifierReturnsTombstone(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "TMB")

	// Create through the regular API so the number comes from the real
	// workspace counter.
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Issue that will be deleted",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}
	identifier := fmt.Sprintf("%s-%d", "TMB", created.Number)

	// Delete it.
	delReq := withURLParam(newRequest("DELETE", "/api/issues/"+created.ID, nil), "id", created.ID)
	dw := httptest.NewRecorder()
	testHandler.DeleteIssue(dw, delReq)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("DeleteIssue: expected 204, got %d: %s", dw.Code, dw.Body.String())
	}

	// The deleted identifier answers with a tombstone.
	getReq := withURLParam(newRequest("GET", "/api/issues/"+identifier, nil), "id", identifier)
	gw := httptest.NewRecorder()
	testHandler.GetIssue(gw, getReq)
	if gw.Code != http.StatusOK {
		t.Fatalf("GetIssue deleted identifier: expected 200 tombstone, got %d: %s", gw.Code, gw.Body.String())
	}
	var tombstone struct {
		Identifier string    `json:"identifier"`
		Number     int32     `json:"number"`
		Status     string    `json:"status"`
		DeletedAt  time.Time `json:"deleted_at"`
	}
	if err := json.NewDecoder(gw.Body).Decode(&tombstone); err != nil {
		t.Fatalf("decode tombstone: %v", err)
	}
	if tombstone.Identifier != identifier {
		t.Fatalf("tombstone identifier = %q, want %q", tombstone.Identifier, identifier)
	}
	if tombstone.Number != created.Number {
		t.Fatalf("tombstone number = %d, want %d", tombstone.Number, created.Number)
	}
	if tombstone.Status != "deleted" {
		t.Fatalf("tombstone status = %q, want %q", tombstone.Status, "deleted")
	}
	if tombstone.DeletedAt.IsZero() {
		t.Fatal("tombstone deleted_at is zero")
	}

	// A never-issued number still returns not found — distinguishable from
	// the tombstone without parsing any error text.
	missing := "TMB-99999"
	missingReq := withURLParam(newRequest("GET", "/api/issues/"+missing, nil), "id", missing)
	mw := httptest.NewRecorder()
	testHandler.GetIssue(mw, missingReq)
	if mw.Code != http.StatusNotFound {
		t.Fatalf("GetIssue never-issued number: expected 404, got %d: %s", mw.Code, mw.Body.String())
	}

	// The deleted issue's UUID keeps returning not found: the tombstone is
	// reachable through the identifier, not the (now meaningless) UUID.
	uuidReq := withURLParam(newRequest("GET", "/api/issues/"+created.ID, nil), "id", created.ID)
	uw := httptest.NewRecorder()
	testHandler.GetIssue(uw, uuidReq)
	if uw.Code != http.StatusNotFound {
		t.Fatalf("GetIssue deleted UUID: expected 404, got %d: %s", uw.Code, uw.Body.String())
	}
}

// TestGetIssueIdentifierPrefixMismatchKeepsNotFound pins the tenant shape of
// the tombstone path: an identifier whose prefix does not match the
// workspace's own prefix must not reach the tombstone lookup, mirroring
// resolveIssueByIdentifier's prefix rule for live rows.
func TestGetIssueIdentifierPrefixMismatchKeepsNotFound(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "TMB")

	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Issue used for prefix mismatch",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}

	delReq := withURLParam(newRequest("DELETE", "/api/issues/"+created.ID, nil), "id", created.ID)
	dw := httptest.NewRecorder()
	testHandler.DeleteIssue(dw, delReq)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("DeleteIssue: expected 204, got %d: %s", dw.Code, dw.Body.String())
	}

	// Right number, wrong prefix: must stay not found even though the
	// number has a tombstone under the workspace's own prefix.
	foreign := fmt.Sprintf("XXX-%d", created.Number)
	foreignReq := withURLParam(newRequest("GET", "/api/issues/"+foreign, nil), "id", foreign)
	fw := httptest.NewRecorder()
	testHandler.GetIssue(fw, foreignReq)
	if fw.Code != http.StatusNotFound {
		t.Fatalf("GetIssue foreign prefix: expected 404, got %d: %s", fw.Code, fw.Body.String())
	}
}
