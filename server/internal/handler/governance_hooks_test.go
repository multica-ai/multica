package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCreateComment_GovernancePreCommentAllowAndDeny(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	dir := t.TempDir()
	allowHook := filepath.Join(dir, "allow.sh")
	if err := os.WriteFile(allowHook, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	denyHook := filepath.Join(dir, "deny.sh")
	if err := os.WriteFile(denyHook, []byte("#!/usr/bin/env bash\necho governance gate blocked comment >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	configureWorkspaceGovernance := func(hookPath string) {
		t.Helper()
		settings, _ := json.Marshal(map[string]any{
			"governance": map[string]any{
				"hooks": map[string]string{
					"pre_comment": hookPath,
				},
			},
		})
		if _, err := testPool.Exec(t.Context(), `UPDATE workspace SET settings = $1 WHERE id = $2`, settings, testWorkspaceID); err != nil {
			t.Fatalf("update workspace settings: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(t.Context(), `UPDATE workspace SET settings = '{}'::jsonb WHERE id = $1`, testWorkspaceID)
		})
	}

	issueID := dbfx.Issue(t, "governance pre-comment hook")

	configureWorkspaceGovernance(allowHook)
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "STATUS: DONE\nallowed comment",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("allow hook: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	configureWorkspaceGovernance(denyHook)
	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "STATUS: DONE\nblocked comment",
	})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("deny hook: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "governance gate blocked comment") {
		t.Fatalf("expected hook stderr in response, got %s", w.Body.String())
	}
}

func TestUpdateIssue_GovernancePreStatusDeny(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	dir := t.TempDir()
	denyHook := filepath.Join(dir, "deny-status.sh")
	if err := os.WriteFile(denyHook, []byte("#!/usr/bin/env bash\necho status transition blocked >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	settings, _ := json.Marshal(map[string]any{
		"governance": map[string]any{
			"hooks": map[string]string{
				"pre_status": denyHook,
			},
		},
	})
	if _, err := testPool.Exec(t.Context(), `UPDATE workspace SET settings = $1 WHERE id = $2`, settings, testWorkspaceID); err != nil {
		t.Fatalf("update workspace settings: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(t.Context(), `UPDATE workspace SET settings = '{}'::jsonb WHERE id = $1`, testWorkspaceID)
	})

	issueID := dbfx.Issue(t, "governance pre-status hook", testutil.Cols{"status": "todo"})
	w := httptest.NewRecorder()
	r := newRequest("PATCH", "/api/issues/"+issueID, map[string]any{"status": "in_progress"})
	r = withURLParam(r, "id", issueID)
	testHandler.UpdateIssue(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("deny hook: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "status transition blocked") {
		t.Fatalf("expected hook stderr in response, got %s", w.Body.String())
	}
}
