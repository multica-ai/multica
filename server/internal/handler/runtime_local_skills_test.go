package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeLocalSkillListStore_PreservesSummaries(t *testing.T) {
	store := NewRuntimeLocalSkillListStore()
	req := store.Create("runtime-xyz")

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

	store.Complete(req.ID, parsed.Skills, true)
	got := store.Get(req.ID)
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

func TestRuntimeLocalSkillImportResult_DecodesBundleFiles(t *testing.T) {
	payload := `{"status":"completed","skill":{"name":"Review Helper","description":"Review PRs","content":"# Review","source_path":"~/.claude/skills/review-helper","provider":"claude","files":[{"path":"templates/check.md","content":"body"}]}}`
	r := httptest.NewRequest(http.MethodPost, "/api/daemon/runtimes/rt/local-skills/import/req/result", bytes.NewBufferString(payload))

	var body struct {
		Status string                     `json:"status"`
		Skill  *reportedRuntimeLocalSkill `json:"skill"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Skill == nil {
		t.Fatal("expected skill bundle")
	}
	if body.Skill.Provider != "claude" {
		t.Fatalf("provider = %q", body.Skill.Provider)
	}
	if len(body.Skill.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(body.Skill.Files))
	}
	if body.Skill.Files[0].Path != "templates/check.md" {
		t.Fatalf("path = %q", body.Skill.Files[0].Path)
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
