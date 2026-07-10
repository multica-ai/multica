package clitools

// FIR-1775 §5 — tests for the skill-governance gap tools (skill_diff,
// skill_update, skill_set_ownership).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// resultText extracts the single text payload from a tool result.
func resultText(t *testing.T, res mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("tool returned no content")
	}
	return res.Content[0].Text
}

// TestRegisterSkillGovernanceGapToolsRegistersAll verifies the three gap tools
// are registered under stable names.
func TestRegisterSkillGovernanceGapToolsRegistersAll(t *testing.T) {
	srv := mcp.NewServer("test", "0")
	registerSkillGovernanceGapTools(srv, cli.NewAPIClient("", "", ""))

	for _, name := range []string{"skill_diff", "skill_update", "skill_set_ownership"} {
		if !hasTool(srv, name) {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

// TestSkillDiffRendersUnifiedDiff — skill_diff fetches the version history and
// renders a unified diff for the SKILL.md body and each changed file.
func TestSkillDiffRendersUnifiedDiff(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/sk-1/versions" {
			t.Errorf("hit %s, want /api/skills/sk-1/versions", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"version": "1.1.0",
				"content": "line one\nline two changed\n",
				"files": []map[string]any{
					{"path": "references/notes.md", "content": "new file\n"},
					{"path": "references/same.md", "content": "unchanged\n"},
				},
			},
			{
				"version": "1.0.0",
				"content": "line one\nline two\n",
				"files": []map[string]any{
					{"path": "references/same.md", "content": "unchanged\n"},
				},
			},
		})
	}))
	defer ts.Close()

	srv := mcp.NewServer("test", "0")
	registerSkillGovernanceGapTools(srv, cli.NewAPIClient(ts.URL, "ws-1", "tok"))

	res, err := srv.Call(context.Background(), "skill_diff", map[string]any{
		"skill_id": "sk-1", "from": "1.0.0", "to": "1.1.0",
	})
	if err != nil {
		t.Fatalf("skill_diff call: %v", err)
	}
	if res.IsError {
		t.Fatalf("skill_diff returned error: %v", res.Content)
	}

	text := resultText(t, res)
	var out struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Files []struct {
			Path string `json:"path"`
			Diff string `json:"diff"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal result: %v\n%s", err, text)
	}
	if out.From != "1.0.0" || out.To != "1.1.0" {
		t.Errorf("from/to = %s/%s", out.From, out.To)
	}
	if len(out.Files) != 2 {
		t.Fatalf("got %d changed files, want 2 (SKILL.md + references/notes.md): %+v", len(out.Files), out.Files)
	}
	if out.Files[0].Path != "SKILL.md" {
		t.Errorf("first changed file = %s, want SKILL.md", out.Files[0].Path)
	}
	if !strings.Contains(out.Files[0].Diff, "-line two") || !strings.Contains(out.Files[0].Diff, "+line two changed") {
		t.Errorf("SKILL.md diff missing expected -/+ lines:\n%s", out.Files[0].Diff)
	}
	if out.Files[1].Path != "references/notes.md" {
		t.Errorf("second changed file = %s, want references/notes.md", out.Files[1].Path)
	}
	if !strings.Contains(out.Files[1].Diff, "+new file") {
		t.Errorf("notes.md diff missing added line:\n%s", out.Files[1].Diff)
	}
}

// TestSkillDiffUnknownVersion — a version missing from the history is an error
// result, not a request failure.
func TestSkillDiffUnknownVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"version": "1.0.0", "content": "x"}})
	}))
	defer ts.Close()

	srv := mcp.NewServer("test", "0")
	registerSkillGovernanceGapTools(srv, cli.NewAPIClient(ts.URL, "ws-1", "tok"))

	res, err := srv.Call(context.Background(), "skill_diff", map[string]any{
		"skill_id": "sk-1", "from": "1.0.0", "to": "9.9.9",
	})
	if err != nil {
		t.Fatalf("skill_diff call: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result for unknown version, got: %v", res.Content)
	}
}

// TestSkillUpdateSendsPut — skill_update PUTs the provided fields to
// /api/skills/{id}.
func TestSkillUpdateSendsPut(t *testing.T) {
	var got map[string]any
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "sk-1", "name": "renamed"})
	}))
	defer ts.Close()

	srv := mcp.NewServer("test", "0")
	registerSkillGovernanceGapTools(srv, cli.NewAPIClient(ts.URL, "ws-1", "tok"))

	res, err := srv.Call(context.Background(), "skill_update", map[string]any{
		"skill_id": "sk-1",
		"name":     "renamed",
		"content":  "# New body",
	})
	if err != nil {
		t.Fatalf("skill_update call: %v", err)
	}
	if res.IsError {
		t.Fatalf("skill_update returned error: %v", res.Content)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/skills/sk-1" {
		t.Fatalf("hit %s %s, want PUT /api/skills/sk-1", gotMethod, gotPath)
	}
	if got["name"] != "renamed" || got["content"] != "# New body" {
		t.Errorf("body = %v", got)
	}
	if _, ok := got["description"]; ok {
		t.Errorf("description should not be sent when omitted: %v", got)
	}
}

// TestSkillUpdateRequiresAField — with no updatable field the tool returns an
// error result without making a request.
func TestSkillUpdateRequiresAField(t *testing.T) {
	srv := mcp.NewServer("test", "0")
	registerSkillGovernanceGapTools(srv, cli.NewAPIClient("http://unused", "ws-1", "tok"))

	res, err := srv.Call(context.Background(), "skill_update", map[string]any{"skill_id": "sk-1"})
	if err != nil {
		t.Fatalf("skill_update call: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result when no fields provided, got: %v", res.Content)
	}
}

// TestSkillSetOwnershipSendsPut — skill_set_ownership PUTs owner_id and
// approver_ids to /api/skills/{id}/ownership.
func TestSkillSetOwnershipSendsPut(t *testing.T) {
	var got map[string]any
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "sk-1"})
	}))
	defer ts.Close()

	srv := mcp.NewServer("test", "0")
	registerSkillGovernanceGapTools(srv, cli.NewAPIClient(ts.URL, "ws-1", "tok"))

	res, err := srv.Call(context.Background(), "skill_set_ownership", map[string]any{
		"skill_id":     "sk-1",
		"owner_id":     "owner-uuid",
		"approver_ids": []any{"appr-1", "appr-2"},
	})
	if err != nil {
		t.Fatalf("skill_set_ownership call: %v", err)
	}
	if res.IsError {
		t.Fatalf("skill_set_ownership returned error: %v", res.Content)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/skills/sk-1/ownership" {
		t.Fatalf("hit %s %s, want PUT /api/skills/sk-1/ownership", gotMethod, gotPath)
	}
	if got["owner_id"] != "owner-uuid" {
		t.Errorf("owner_id = %v", got["owner_id"])
	}
	approvers, ok := got["approver_ids"].([]any)
	if !ok || len(approvers) != 2 {
		t.Errorf("approver_ids = %v, want 2 entries", got["approver_ids"])
	}
}

// TestSkillSetOwnershipClearApprovers — clear_approvers sends an empty
// approver_ids array (nil would mean "leave unchanged" server-side).
func TestSkillSetOwnershipClearApprovers(t *testing.T) {
	var raw []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "sk-1"})
	}))
	defer ts.Close()

	srv := mcp.NewServer("test", "0")
	registerSkillGovernanceGapTools(srv, cli.NewAPIClient(ts.URL, "ws-1", "tok"))

	res, err := srv.Call(context.Background(), "skill_set_ownership", map[string]any{
		"skill_id": "sk-1", "clear_approvers": true,
	})
	if err != nil {
		t.Fatalf("skill_set_ownership call: %v", err)
	}
	if res.IsError {
		t.Fatalf("skill_set_ownership returned error: %v", res.Content)
	}
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	approvers, ok := got["approver_ids"].([]any)
	if !ok || len(approvers) != 0 {
		t.Errorf("approver_ids = %v, want explicit empty array", got["approver_ids"])
	}
}
