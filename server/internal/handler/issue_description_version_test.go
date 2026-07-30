package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// withVersionParams sets both route params at once. withURLParam allocates a
// fresh chi route context per call, so chaining it would drop the first param.
func withVersionParams(req *http.Request, issueID, versionID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	rctx.URLParams.Add("versionId", versionID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// createIssueForVersioning returns a fresh issue and cleans up its version rows.
func createIssueForVersioning(t *testing.T, title, description string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       title,
		"description": description,
		"status":      "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM issue_description_version WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue.ID
}

func updateDescription(t *testing.T, issueID, description string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"description": description})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func listVersions(t *testing.T, issueID string) []DescriptionVersionResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/description/versions", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueDescriptionVersions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueDescriptionVersions: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var versions []DescriptionVersionResponse
	json.NewDecoder(w.Body).Decode(&versions)
	return versions
}

// The first tracked edit must produce TWO rows: the seeded original plus the
// edit. Without the seed the most valuable diff in the feature — the human's
// original request vs. the first rewrite — would have no left-hand side.
func TestDescriptionVersionSeedsOriginal(t *testing.T) {
	issueID := createIssueForVersioning(t, "Seed original", "original request\nsecond line")
	updateDescription(t, issueID, "a full rewrite")

	versions := listVersions(t, issueID)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions (original + edit), got %d", len(versions))
	}
	// Newest first.
	if versions[1].IsOriginal != true {
		t.Errorf("oldest version should be flagged is_original")
	}
	if versions[0].IsOriginal != false {
		t.Errorf("newest version should not be flagged is_original")
	}
	if versions[0].AddedLines != 1 || versions[0].RemovedLines != 2 {
		t.Errorf("expected (+1, -2), got (+%d, -%d)", versions[0].AddedLines, versions[0].RemovedLines)
	}

	original := getVersion(t, issueID, versions[1].ID)
	if original.Content == nil || *original.Content != "original request\nsecond line" {
		t.Errorf("original snapshot lost: %v", original.Content)
	}
}

// The editor autosaves on a 1.5s debounce. Consecutive writes by the same actor
// must collapse into ONE version, or a single sitting would bury the history
// panel and the timeline.
func TestDescriptionVersionCoalescesSameActorSession(t *testing.T) {
	issueID := createIssueForVersioning(t, "Coalesce", "start")

	updateDescription(t, issueID, "start\nline one")
	updateDescription(t, issueID, "start\nline one\nline two")
	updateDescription(t, issueID, "start\nline one\nline two\nline three")

	versions := listVersions(t, issueID)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions (original + one coalesced session), got %d", len(versions))
	}
	// Counts are recomputed against the session's parent each time, never
	// accumulated: three lines added over three writes reads as +3, not +6.
	if versions[0].AddedLines != 3 || versions[0].RemovedLines != 0 {
		t.Errorf("expected (+3, -0) for the session, got (+%d, -%d)",
			versions[0].AddedLines, versions[0].RemovedLines)
	}
}

// A session that adds a line and then takes it back must settle at zero rather
// than reporting the churn, because counts are recomputed against the parent.
func TestDescriptionVersionSessionCountsAreNotCumulative(t *testing.T) {
	issueID := createIssueForVersioning(t, "Churn", "keep me")

	updateDescription(t, issueID, "keep me\ntypo line")
	updateDescription(t, issueID, "keep me")

	versions := listVersions(t, issueID)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].AddedLines != 0 || versions[0].RemovedLines != 0 {
		t.Errorf("round-tripped session should settle at (+0, -0), got (+%d, -%d)",
			versions[0].AddedLines, versions[0].RemovedLines)
	}
}

// Restore is an ordinary edit: it appends a new version and never deletes
// history, so the restored-away content stays reachable.
func TestDescriptionVersionRestoreAppendsAndKeepsHistory(t *testing.T) {
	issueID := createIssueForVersioning(t, "Restore", "the original request")
	updateDescription(t, issueID, "an agent rewrote everything")

	versions := listVersions(t, issueID)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	originalID := versions[1].ID

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/description/versions/"+originalID+"/restore", nil)
	req = withVersionParams(req, issueID, originalID)
	testHandler.RestoreIssueDescriptionVersion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("restore: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var restored IssueResponse
	json.NewDecoder(w.Body).Decode(&restored)
	if restored.Description == nil || *restored.Description != "the original request" {
		t.Fatalf("restore did not apply: %v", restored.Description)
	}

	after := listVersions(t, issueID)
	if len(after) != 3 {
		t.Fatalf("restore should append a version, not rewind: got %d versions", len(after))
	}
	// The rewrite we restored away from is still readable.
	rewrite := getVersion(t, issueID, after[1].ID)
	if rewrite.Content == nil || *rewrite.Content != "an agent rewrote everything" {
		t.Errorf("restore destroyed the version it replaced: %v", rewrite.Content)
	}
}

// A version id from another issue must not be readable through this issue's
// path, even inside the same workspace.
func TestDescriptionVersionRejectsCrossIssueID(t *testing.T) {
	issueA := createIssueForVersioning(t, "Owner", "a")
	issueB := createIssueForVersioning(t, "Other", "b")
	updateDescription(t, issueA, "a changed")
	updateDescription(t, issueB, "b changed")

	foreignID := listVersions(t, issueB)[0].ID

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueA+"/description/versions/"+foreignID, nil)
	req = withVersionParams(req, issueA, foreignID)
	testHandler.GetIssueDescriptionVersion(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for a cross-issue version id, got %d", w.Code)
	}
}

func getVersion(t *testing.T, issueID, versionID string) DescriptionVersionResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/description/versions/"+versionID, nil)
	req = withVersionParams(req, issueID, versionID)
	testHandler.GetIssueDescriptionVersion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssueDescriptionVersion: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var version DescriptionVersionResponse
	json.NewDecoder(w.Body).Decode(&version)
	return version
}
