package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCreateAgentRequest_FixedRepoFields(t *testing.T) {
	t.Parallel()

	body := `{
		"name": "test-agent",
		"runtime_id": "00000000-0000-0000-0000-000000000001",
		"fixed_repo_enabled": true,
		"fixed_repo_paths": ["/data/repos/a", "/data/repos/b"],
		"vcs_type": "git",
		"cleanup_script": "/data/repos/scripts/clean.sh"
	}`

	var req CreateAgentRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if req.FixedRepoEnabled != true {
		t.Fatal("FixedRepoEnabled should be true")
	}
	if len(req.FixedRepoPaths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(req.FixedRepoPaths))
	}
	if req.VCSType != "git" {
		t.Fatalf("expected vcs_type 'git', got %q", req.VCSType)
	}
	if req.CleanupScript != "/data/repos/scripts/clean.sh" {
		t.Fatalf("expected cleanup_script, got %q", req.CleanupScript)
	}
}

func TestCreateAgentRequest_FixedRepoFieldsOmitted(t *testing.T) {
	t.Parallel()

	body := `{
		"name": "test-agent",
		"runtime_id": "00000000-0000-0000-0000-000000000001"
	}`

	var req CreateAgentRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if req.FixedRepoEnabled != false {
		t.Fatal("FixedRepoEnabled should default to false")
	}
	if req.FixedRepoPaths != nil {
		t.Fatal("FixedRepoPaths should be nil when omitted")
	}
	if req.VCSType != "" {
		t.Fatal("VCSType should be empty when omitted")
	}
	if req.CleanupScript != "" {
		t.Fatal("CleanupScript should be empty when omitted")
	}
}

func TestUpdateAgentRequest_FixedRepoFields(t *testing.T) {
	t.Parallel()

	body := `{
		"fixed_repo_enabled": true,
		"fixed_repo_paths": ["/data/repos/x"],
		"vcs_type": "p4",
		"cleanup_script": ""
	}`

	var req UpdateAgentRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if req.FixedRepoEnabled == nil || *req.FixedRepoEnabled != true {
		t.Fatal("FixedRepoEnabled should be true")
	}
	if len(req.FixedRepoPaths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(req.FixedRepoPaths))
	}
	if req.VCSType == nil || *req.VCSType != "p4" {
		t.Fatal("VCSType should be 'p4'")
	}
	if req.CleanupScript == nil || *req.CleanupScript != "" {
		t.Fatal("CleanupScript should be empty string")
	}
}

func TestAgentResponse_FixedRepoFields(t *testing.T) {
	t.Parallel()

	resp := AgentResponse{
		FixedRepoEnabled: true,
		FixedRepoPaths:   []string{"/data/repos/a"},
		VCSType:          "svn",
		CleanupScript:    "/clean.sh",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal back failed: %v", err)
	}

	if v, ok := out["fixed_repo_enabled"].(bool); !ok || !v {
		t.Fatal("fixed_repo_enabled should be true")
	}
	paths, ok := out["fixed_repo_paths"].([]interface{})
	if !ok || len(paths) != 1 || paths[0] != "/data/repos/a" {
		t.Fatal("fixed_repo_paths should contain the path")
	}
	if v, ok := out["vcs_type"].(string); !ok || v != "svn" {
		t.Fatal("vcs_type should be 'svn'")
	}
	if v, ok := out["cleanup_script"].(string); !ok || v != "/clean.sh" {
		t.Fatal("cleanup_script should be '/clean.sh'")
	}
}

func TestUpdateAgentRequest_FixedRepoPathsClear(t *testing.T) {
	t.Parallel()

	// Sending an empty array should result in a non-nil empty slice.
	body := `{"fixed_repo_paths": []}`
	var req UpdateAgentRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.FixedRepoPaths == nil {
		t.Fatal("FixedRepoPaths should be non-nil empty slice, not nil")
	}
	if len(req.FixedRepoPaths) != 0 {
		t.Fatal("FixedRepoPaths should be empty")
	}
}

func TestUpdateAgentRequest_FixedRepoFieldsOmitted(t *testing.T) {
	t.Parallel()

	body := `{"name": "new-name"}`
	var req UpdateAgentRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if req.FixedRepoEnabled != nil {
		t.Fatal("FixedRepoEnabled should be nil when omitted")
	}
	if req.FixedRepoPaths != nil {
		t.Fatal("FixedRepoPaths should be nil when omitted")
	}
	if req.VCSType != nil {
		t.Fatal("VCSType should be nil when omitted")
	}
	if req.CleanupScript != nil {
		t.Fatal("CleanupScript should be nil when omitted")
	}
}
