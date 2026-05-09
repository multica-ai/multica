package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newRequestAsUser(userID, method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

func createRuntimeLocalSkillTestRuntime(t *testing.T, ownerID string) string {
	t.Helper()

	runtimeName := fmt.Sprintf("runtime-local-skill-%d", time.Now().UnixNano())
	daemonID := fmt.Sprintf("runtime-local-skill-daemon-%d", time.Now().UnixNano())

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, $2, $3, 'local', 'claude', 'online', 'Runtime Local Skills Test', '{}'::jsonb, $4, now())
		RETURNING id
	`, testWorkspaceID, daemonID, runtimeName, ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("create local runtime: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	return runtimeID
}

func createRuntimeLocalSkillTestMember(t *testing.T, role string) string {
	t.Helper()

	email := fmt.Sprintf("runtime-local-skills-%d@multica.ai", time.Now().UnixNano())
	name := fmt.Sprintf("Runtime Local Skills %s", role)

	var userID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, name, email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
	`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("create member: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return userID
}

func countSkillsByName(t *testing.T, name string) int {
	t.Helper()

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM skill
		WHERE workspace_id = $1 AND name = $2
	`, testWorkspaceID, name).Scan(&count); err != nil {
		t.Fatalf("count skills: %v", err)
	}

	return count
}

func countSkillFiles(t *testing.T, skillID string) int {
	t.Helper()

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM skill_file
		WHERE skill_id = $1
	`, skillID).Scan(&count); err != nil {
		t.Fatalf("count skill files: %v", err)
	}

	return count
}

func TestInMemoryLocalSkillListStore_PreservesSummaries(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLocalSkillListStore()
	req, err := store.Create(ctx, "runtime-xyz")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	body := map[string]any{
		"status":    "completed",
		"supported": true,
		"skills": []map[string]any{
			{
				"key":         "review-helper",
				"name":        "Review Helper",
				"description": "Review PRs",
				"source_path": "~/.claude/skills/review-helper",
				"provider":    "claude",
				"file_count":  2,
			},
		},
	}
	raw, _ := json.Marshal(body)

	var parsed struct {
		Skills []RuntimeLocalSkillSummary `json:"skills"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal report body: %v", err)
	}

	if err := store.Complete(ctx, req.ID, parsed.Skills, true); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected stored result")
	}
	if len(got.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got.Skills))
	}
	if got.Skills[0].SourcePath != "~/.claude/skills/review-helper" {
		t.Fatalf("source_path = %q", got.Skills[0].SourcePath)
	}
	if got.Skills[0].FileCount != 2 {
		t.Fatalf("file_count = %d", got.Skills[0].FileCount)
	}
}

func TestInMemoryLocalSkillListStore_TimesOutRunningRequests(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLocalSkillListStore()
	req, err := store.Create(ctx, "runtime-xyz")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	req.Status = RuntimeLocalSkillRunning
	startedAt := time.Now().Add(-61 * time.Second)
	req.RunStartedAt = &startedAt

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected stored request")
	}
	if got.Status != RuntimeLocalSkillTimeout {
		t.Fatalf("expected timeout, got %s", got.Status)
	}
	if got.Error == "" {
		t.Fatal("expected timeout error")
	}
}

func TestInMemoryLocalSkillImportStore_TimesOutRunningRequests(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLocalSkillImportStore()
	req, err := store.Create(ctx, "runtime-xyz", "user-1", "review-helper", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	req.Status = RuntimeLocalSkillRunning
	startedAt := time.Now().Add(-61 * time.Second)
	req.RunStartedAt = &startedAt

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected stored request")
	}
	if got.Status != RuntimeLocalSkillTimeout {
		t.Fatalf("expected timeout, got %s", got.Status)
	}
	if got.Error == "" {
		t.Fatal("expected timeout error")
	}
}

func TestInitiateListLocalSkills_RequiresRuntimeOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	adminUserID := createRuntimeLocalSkillTestMember(t, "admin")

	w := httptest.NewRecorder()
	req := withURLParams(
		newRequestAsUser(adminUserID, http.MethodPost, "/api/runtimes/"+runtimeID+"/local-skills", nil),
		"runtimeId", runtimeID,
	)

	testHandler.InitiateListLocalSkills(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetLocalSkillImportRequest_RequiresRuntimeOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	adminUserID := createRuntimeLocalSkillTestMember(t, "admin")
	importReq, err := testHandler.LocalSkillImportStore.Create(context.Background(), runtimeID, testUserID, "review-helper", nil, nil)
	if err != nil {
		t.Fatalf("create import request: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParams(
		newRequestAsUser(adminUserID, http.MethodGet, "/api/runtimes/"+runtimeID+"/local-skills/import/"+importReq.ID, nil),
		"runtimeId", runtimeID,
		"requestId", importReq.ID,
	)

	testHandler.GetLocalSkillImportRequest(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRuntimeLocalSkillImportFlow_EndToEnd(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	w := httptest.NewRecorder()
	initReq := withURLParams(
		newRequestAsUser(testUserID, http.MethodPost, "/api/runtimes/"+runtimeID+"/local-skills/import", map[string]any{
			"skill_key":   "review-helper",
			"name":        "Imported Review Helper",
			"description": "Imported description",
		}),
		"runtimeId", runtimeID,
	)
	testHandler.InitiateImportLocalSkill(w, initReq)
	if w.Code != http.StatusOK {
		t.Fatalf("InitiateImportLocalSkill: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var importReq RuntimeLocalSkillImportRequest
	if err := json.NewDecoder(w.Body).Decode(&importReq); err != nil {
		t.Fatalf("decode import request: %v", err)
	}

	w = httptest.NewRecorder()
	heartbeatReq := newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
	}, testWorkspaceID, "runtime-local-skills-daemon")
	testHandler.DaemonHeartbeat(w, heartbeatReq)
	if w.Code != http.StatusOK {
		t.Fatalf("DaemonHeartbeat: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var heartbeatResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&heartbeatResp); err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}
	pending, ok := heartbeatResp["pending_local_skill_import"].(map[string]any)
	if !ok {
		t.Fatalf("expected pending_local_skill_import, got %v", heartbeatResp)
	}
	if pending["id"] != importReq.ID {
		t.Fatalf("pending id = %v, want %s", pending["id"], importReq.ID)
	}
	if pending["skill_key"] != "review-helper" {
		t.Fatalf("pending skill_key = %v", pending["skill_key"])
	}
	if _, ok := pending["name"]; ok {
		t.Fatalf("heartbeat payload should not include name: %v", pending)
	}
	if _, ok := pending["description"]; ok {
		t.Fatalf("heartbeat payload should not include description: %v", pending)
	}

	w = httptest.NewRecorder()
	reportReq := withURLParams(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/local-skills/import/"+importReq.ID+"/result", map[string]any{
			"status": "completed",
			"skill": map[string]any{
				"name":        "Original Review Helper",
				"description": "Original description",
				"content":     "# Review Helper",
				"source_path": "~/.claude/skills/review-helper",
				"provider":    "claude",
				"files": []map[string]any{
					{
						"path":    "templates/check.md",
						"content": "body",
					},
				},
			},
		}, testWorkspaceID, "runtime-local-skills-daemon"),
		"runtimeId", runtimeID,
		"requestId", importReq.ID,
	)
	testHandler.ReportLocalSkillImportResult(w, reportReq)
	if w.Code != http.StatusOK {
		t.Fatalf("ReportLocalSkillImportResult: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	pollReq := withURLParams(
		newRequestAsUser(testUserID, http.MethodGet, "/api/runtimes/"+runtimeID+"/local-skills/import/"+importReq.ID, nil),
		"runtimeId", runtimeID,
		"requestId", importReq.ID,
	)
	testHandler.GetLocalSkillImportRequest(w, pollReq)
	if w.Code != http.StatusOK {
		t.Fatalf("GetLocalSkillImportRequest: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var completed RuntimeLocalSkillImportRequest
	if err := json.NewDecoder(w.Body).Decode(&completed); err != nil {
		t.Fatalf("decode poll response: %v", err)
	}
	if completed.Status != RuntimeLocalSkillCompleted {
		t.Fatalf("expected completed status, got %s", completed.Status)
	}
	if completed.Skill == nil {
		t.Fatal("expected imported skill")
	}
	if completed.Skill.Name != "Imported Review Helper" {
		t.Fatalf("imported name = %q", completed.Skill.Name)
	}
	if completed.Skill.Description != "Imported description" {
		t.Fatalf("imported description = %q", completed.Skill.Description)
	}
	if got := countSkillFiles(t, completed.Skill.ID); got != 1 {
		t.Fatalf("expected 1 imported file, got %d", got)
	}
}

func TestReportLocalSkillImportResult_IgnoresTimedOutRequests(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	ctx := context.Background()
	importReq, err := testHandler.LocalSkillImportStore.Create(
		ctx,
		runtimeID,
		testUserID,
		"review-helper",
		cleanOptionalString(ptr("Timed Out Import")),
		cleanOptionalString(ptr("Should not be created")),
	)
	if err != nil {
		t.Fatalf("create import request: %v", err)
	}
	importReq.Status = RuntimeLocalSkillRunning
	startedAt := time.Now().Add(-61 * time.Second)
	importReq.RunStartedAt = &startedAt

	timedOut, err := testHandler.LocalSkillImportStore.Get(ctx, importReq.ID)
	if err != nil {
		t.Fatalf("get import request: %v", err)
	}
	if timedOut == nil || timedOut.Status != RuntimeLocalSkillTimeout {
		t.Fatalf("expected timed out request, got %#v", timedOut)
	}

	beforeCount := countSkillsByName(t, "Timed Out Import")

	w := httptest.NewRecorder()
	reportReq := withURLParams(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/local-skills/import/"+importReq.ID+"/result", map[string]any{
			"status": "completed",
			"skill": map[string]any{
				"name":        "Original Review Helper",
				"description": "Original description",
				"content":     "# Review Helper",
				"source_path": "~/.claude/skills/review-helper",
				"provider":    "claude",
			},
		}, testWorkspaceID, "runtime-local-skills-daemon"),
		"runtimeId", runtimeID,
		"requestId", importReq.ID,
	)
	testHandler.ReportLocalSkillImportResult(w, reportReq)
	if w.Code != http.StatusOK {
		t.Fatalf("ReportLocalSkillImportResult: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	afterCount := countSkillsByName(t, "Timed Out Import")
	if afterCount != beforeCount {
		t.Fatalf("expected timed out report to be ignored, count before=%d after=%d", beforeCount, afterCount)
	}
}

func TestReportLocalSkillImportResult_RejectsCrossWorkspaceDaemonToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	importReq, err := testHandler.LocalSkillImportStore.Create(context.Background(), runtimeID, testUserID, "review-helper", nil, nil)
	if err != nil {
		t.Fatalf("create import request: %v", err)
	}

	w := httptest.NewRecorder()
	reportReq := withURLParams(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/local-skills/import/"+importReq.ID+"/result", map[string]any{
			"status": "failed",
			"error":  "forbidden",
		}, "00000000-0000-0000-0000-000000000000", "attacker-daemon"),
		"runtimeId", runtimeID,
		"requestId", importReq.ID,
	)
	testHandler.ReportLocalSkillImportResult(w, reportReq)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCleanOptionalString(t *testing.T) {
	if got := cleanOptionalString(nil); got != nil {
		t.Fatalf("expected nil, got %q", *got)
	}

	raw := "  "
	if got := cleanOptionalString(&raw); got != nil {
		t.Fatalf("expected nil for whitespace-only value, got %q", *got)
	}

	value := "  Review Helper  "
	got := cleanOptionalString(&value)
	if got == nil || *got != "Review Helper" {
		t.Fatalf("expected trimmed value, got %#v", got)
	}
}

func ptr[T any](value T) *T {
	return &value
}
