package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// withRuntimeID supplies the {runtimeId} path parameter these handlers read from
// the chi route context, which a directly-invoked handler does not have. Its
// peers in this package do the same.
func withRuntimeID(req *http.Request, runtimeID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestReportRuntimeResumeWarning covers the daemon-authenticated endpoint that
// records a prior-session continuity warning on the runtime.
//
// DB-backed, like its peers in this package: skipped when no database is
// available locally, exercised by the backend CI job.
func TestReportRuntimeResumeWarning(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const daemonID = "resume-warning-daemon"

	// Register a runtime through the real register handler so the row is exactly
	// what a daemon owns.
	w := httptest.NewRecorder()
	testHandler.DaemonRegister(w, newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    daemonID,
		"device_name":  "resume-warning-device",
		"runtimes": []map[string]any{
			{"name": "resume-warning-runtime", "type": "codex", "version": "1.0.0", "status": "online"},
		},
	}, testWorkspaceID, daemonID))
	if w.Code != http.StatusOK {
		t.Fatalf("DaemonRegister: %d: %s", w.Code, w.Body.String())
	}
	// Read the row back rather than decoding the register response: the response
	// shape is the daemon's contract, not this endpoint's, and the runtime id the
	// route needs is simply the row this daemon registered.
	var runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent_runtime WHERE daemon_id = $1 LIMIT 1`, daemonID).Scan(&runtimeID); err != nil {
		t.Fatalf("find registered runtime: %v", err)
	}

	// Seed an unrelated metadata key so the merge can be proven not to clobber it.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET metadata = COALESCE(metadata, '{}'::jsonb) || '{"capabilities":["mcp"]}'::jsonb WHERE id = $1`,
		runtimeID); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	readWarning := func(t *testing.T) (map[string]any, map[string]any) {
		t.Helper()
		var raw []byte
		if err := testPool.QueryRow(ctx, `SELECT metadata FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&raw); err != nil {
			t.Fatalf("read metadata: %v", err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(raw, &metadata); err != nil {
			t.Fatalf("decode metadata %s: %v", raw, err)
		}
		warning, _ := metadata["resume_warning"].(map[string]any)
		return metadata, warning
	}

	// A. The owning daemon reports; the server builds the stored object.
	w = httptest.NewRecorder()
	testHandler.ReportRuntimeResumeWarning(w, withRuntimeID(newDaemonTokenRequest("POST",
		"/api/daemon/runtimes/"+runtimeID+"/resume-warning",
		map[string]any{"code": ResumeWarningPriorSessionUnavailable, "task_id": "task-1"},
		testWorkspaceID, daemonID), runtimeID))
	if w.Code != http.StatusOK {
		t.Fatalf("report: %d: %s", w.Code, w.Body.String())
	}
	metadata, warning := readWarning(t)
	if warning["code"] != ResumeWarningPriorSessionUnavailable || warning["task_id"] != "task-1" {
		t.Fatalf("stored warning = %#v", warning)
	}
	if occurredAt, _ := warning["occurred_at"].(string); occurredAt == "" {
		t.Fatalf("stored warning has no occurred_at: %#v", warning)
	}
	// B. Every other metadata key survives the merge.
	capabilities, ok := metadata["capabilities"].([]any)
	if !ok || len(capabilities) != 1 || capabilities[0] != "mcp" {
		t.Fatalf("merge clobbered existing metadata: %#v", metadata)
	}
	if _, ok := metadata["cli_version"]; !ok {
		t.Fatalf("merge dropped registration metadata: %#v", metadata)
	}

	// D. An unknown code is refused and changes nothing.
	w = httptest.NewRecorder()
	testHandler.ReportRuntimeResumeWarning(w, withRuntimeID(newDaemonTokenRequest("POST",
		"/api/daemon/runtimes/"+runtimeID+"/resume-warning",
		map[string]any{"code": "something_else", "task_id": "task-2"},
		testWorkspaceID, daemonID), runtimeID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown code: %d: %s", w.Code, w.Body.String())
	}
	if _, after := readWarning(t); after["task_id"] != "task-1" {
		t.Fatalf("a rejected report mutated metadata: %#v", after)
	}

	// C. A daemon authenticated for another workspace cannot report on this one.
	w = httptest.NewRecorder()
	testHandler.ReportRuntimeResumeWarning(w, withRuntimeID(newDaemonTokenRequest("POST",
		"/api/daemon/runtimes/"+runtimeID+"/resume-warning",
		map[string]any{"code": ResumeWarningPriorSessionUnavailable, "task_id": "task-3"},
		"00000000-0000-0000-0000-000000000000", daemonID), runtimeID))
	if w.Code == http.StatusOK {
		t.Fatalf("a foreign workspace reported on this runtime: %d: %s", w.Code, w.Body.String())
	}
	if _, after := readWarning(t); after["task_id"] != "task-1" {
		t.Fatalf("an unauthorized report mutated metadata: %#v", after)
	}
}
