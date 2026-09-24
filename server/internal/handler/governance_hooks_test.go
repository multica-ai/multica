package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// governanceTestMu serialises handler governance integration tests. They mutate
// workspace.settings on a dedicated workspace, but the mutex keeps hook env
// setup/teardown from racing other packages that share the process-wide pool.
var governanceTestMu sync.Mutex

type governanceTestEnv struct {
	workspaceID string
	fixture     *testutil.Fixture
}

func setupGovernanceTestWorkspace(t *testing.T) governanceTestEnv {
	t.Helper()
	ctx := context.Background()

	slug := fmt.Sprintf("gov-hooks-%s", strings.ReplaceAll(t.Name(), "/", "-"))
	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'GOV')
		RETURNING id
	`, "Governance hook tests "+t.Name(), slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create governance test workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, testUserID); err != nil {
		t.Fatalf("add governance test member: %v", err)
	}

	return governanceTestEnv{
		workspaceID: workspaceID,
		fixture:     testutil.New(testPool, workspaceID, testUserID),
	}
}

func newRequestForWorkspace(workspaceID, method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	return req
}

func setWorkspaceGovernanceHooks(t *testing.T, workspaceID string, hooks map[string]string, timeoutSeconds *int) {
	t.Helper()
	ctx := context.Background()

	gov := map[string]any{"hooks": hooks}
	if timeoutSeconds != nil {
		gov["timeout_seconds"] = *timeoutSeconds
	}
	settings, err := json.Marshal(map[string]any{"governance": gov})
	if err != nil {
		t.Fatalf("marshal governance settings: %v", err)
	}

	var previous []byte
	if err := testPool.QueryRow(ctx, `SELECT settings FROM workspace WHERE id = $1`, workspaceID).Scan(&previous); err != nil {
		t.Fatalf("read workspace settings: %v", err)
	}

	if _, err := testPool.Exec(ctx, `UPDATE workspace SET settings = $1 WHERE id = $2`, settings, workspaceID); err != nil {
		t.Fatalf("update workspace governance settings: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previous, workspaceID)
	})
}

func TestCreateComment_GovernancePreCommentAllowAndDeny(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	governanceTestMu.Lock()
	defer governanceTestMu.Unlock()

	env := setupGovernanceTestWorkspace(t)

	dir := t.TempDir()
	allowHook := filepath.Join(dir, "allow.sh")
	if err := os.WriteFile(allowHook, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	denyHook := filepath.Join(dir, "deny.sh")
	if err := os.WriteFile(denyHook, []byte("#!/usr/bin/env bash\necho governance gate blocked comment >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	issueID := env.fixture.Issue(t, "governance pre-comment hook")

	setWorkspaceGovernanceHooks(t, env.workspaceID, map[string]string{"pre_comment": allowHook}, nil)
	w := httptest.NewRecorder()
	r := withURLParam(newRequestForWorkspace(env.workspaceID, "POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "STATUS: DONE\nallowed comment",
	}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("allow hook: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	setWorkspaceGovernanceHooks(t, env.workspaceID, map[string]string{"pre_comment": denyHook}, nil)
	w = httptest.NewRecorder()
	r = withURLParam(newRequestForWorkspace(env.workspaceID, "POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "STATUS: DONE\nblocked comment",
	}), "id", issueID)
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
	governanceTestMu.Lock()
	defer governanceTestMu.Unlock()

	env := setupGovernanceTestWorkspace(t)

	dir := t.TempDir()
	denyHook := filepath.Join(dir, "deny-status.sh")
	if err := os.WriteFile(denyHook, []byte("#!/usr/bin/env bash\necho status transition blocked >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	setWorkspaceGovernanceHooks(t, env.workspaceID, map[string]string{"pre_status": denyHook}, nil)

	issueID := env.fixture.Issue(t, "governance pre-status hook", testutil.Cols{"status": "todo"})
	w := httptest.NewRecorder()
	r := withURLParam(newRequestForWorkspace(env.workspaceID, "PATCH", "/api/issues/"+issueID, map[string]any{"status": "in_progress"}), "id", issueID)
	testHandler.UpdateIssue(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("deny hook: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "status transition blocked") {
		t.Fatalf("expected hook stderr in response, got %s", w.Body.String())
	}
}
