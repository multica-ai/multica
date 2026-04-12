package daemon

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestNormalizeServerBaseURL(t *testing.T) {
	t.Parallel()

	got, err := NormalizeServerBaseURL("ws://localhost:8080/ws")
	if err != nil {
		t.Fatalf("NormalizeServerBaseURL returned error: %v", err)
	}
	if got != "http://localhost:8080" {
		t.Fatalf("expected http://localhost:8080, got %s", got)
	}
}

func TestBuildPromptContainsIssueID(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	prompt := BuildPrompt(Task{
		IssueID: issueID,
		Agent: &AgentData{
			Name: "Local Codex",
			Skills: []SkillData{
				{Name: "Concise", Content: "Be concise."},
			},
		},
	})

	// Prompt should contain the issue ID and CLI hint.
	for _, want := range []string{
		issueID,
		"multica issue get",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}

	// Skills should NOT be inlined in the prompt (they're in runtime config).
	for _, absent := range []string{"## Agent Skills", "Be concise."} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q (skills are in runtime config)", absent)
		}
	}
}

func TestBuildPromptNoIssueDetails(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID: "test-id",
		Agent:   &AgentData{Name: "Test"},
	})

	// Prompt should not contain issue title/description (agent fetches via CLI).
	for _, absent := range []string{"**Issue:**", "**Summary:**"} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q — agent fetches details via CLI", absent)
		}
	}
}

func TestIsWorkspaceNotFoundError(t *testing.T) {
	t.Parallel()

	err := &requestError{
		Method:     http.MethodPost,
		Path:       "/api/daemon/register",
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"workspace not found"}`,
	}
	if !isWorkspaceNotFoundError(err) {
		t.Fatal("expected workspace not found error to be recognized")
	}

	if isWorkspaceNotFoundError(&requestError{StatusCode: http.StatusInternalServerError, Body: `{"error":"workspace not found"}`}) {
		t.Fatal("did not expect 500 to be treated as workspace not found")
	}
}

// TestLoadConfigClaudeExtraEnv verifies that MULTICA_CLAUDE_API_KEY and
// MULTICA_CLAUDE_BASE_URL are populated into AgentEntry.ExtraEnv as the
// correct Anthropic env var names.
func TestLoadConfigClaudeExtraEnv(t *testing.T) {
	// Create a temporary fake claude binary so exec.LookPath succeeds.
	tmp := t.TempDir()
	fakeClaude := tmp + "/claude"
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("create fake claude: %v", err)
	}

	t.Setenv("MULTICA_CLAUDE_PATH", fakeClaude)
	t.Setenv("MULTICA_CLAUDE_API_KEY", "minimax-test-key")
	t.Setenv("MULTICA_CLAUDE_BASE_URL", "https://api.minimax.io/anthropic")
	// Ensure other agents are not found so we don't need more fake binaries.
	for _, v := range []string{
		"MULTICA_CODEX_PATH", "MULTICA_OPENCODE_PATH",
		"MULTICA_OPENCLAW_PATH", "MULTICA_HERMES_PATH",
	} {
		t.Setenv(v, "/nonexistent/binary-"+v)
	}

	cfg, err := LoadConfig(Overrides{})
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	claudeEntry, ok := cfg.Agents["claude"]
	if !ok {
		t.Fatal("expected claude entry in config")
	}
	if claudeEntry.ExtraEnv["ANTHROPIC_AUTH_TOKEN"] != "minimax-test-key" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q, want %q", claudeEntry.ExtraEnv["ANTHROPIC_AUTH_TOKEN"], "minimax-test-key")
	}
	if claudeEntry.ExtraEnv["ANTHROPIC_BASE_URL"] != "https://api.minimax.io/anthropic" {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want %q", claudeEntry.ExtraEnv["ANTHROPIC_BASE_URL"], "https://api.minimax.io/anthropic")
	}
}

// TestLoadConfigOpenclawExtraEnv verifies that MULTICA_OPENCLAW_API_KEY is
// mapped to MINIMAX_API_KEY in AgentEntry.ExtraEnv.
func TestLoadConfigOpenclawExtraEnv(t *testing.T) {
	tmp := t.TempDir()
	fakeOpenclaw := tmp + "/openclaw"
	if err := os.WriteFile(fakeOpenclaw, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("create fake openclaw: %v", err)
	}

	t.Setenv("MULTICA_OPENCLAW_PATH", fakeOpenclaw)
	t.Setenv("MULTICA_OPENCLAW_API_KEY", "minimax-openclaw-key")
	for _, v := range []string{
		"MULTICA_CLAUDE_PATH", "MULTICA_CODEX_PATH",
		"MULTICA_OPENCODE_PATH", "MULTICA_HERMES_PATH",
	} {
		t.Setenv(v, "/nonexistent/binary-"+v)
	}

	cfg, err := LoadConfig(Overrides{})
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	entry, ok := cfg.Agents["openclaw"]
	if !ok {
		t.Fatal("expected openclaw entry in config")
	}
	if entry.ExtraEnv["MINIMAX_API_KEY"] != "minimax-openclaw-key" {
		t.Errorf("MINIMAX_API_KEY = %q, want %q", entry.ExtraEnv["MINIMAX_API_KEY"], "minimax-openclaw-key")
	}
}

// TestAgentEntryExtraEnvEmpty verifies that an agent entry without API key
// env vars set has a nil or empty ExtraEnv — no spurious keys injected.
func TestAgentEntryExtraEnvEmpty(t *testing.T) {
	tmp := t.TempDir()
	fakeClaude := tmp + "/claude"
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("create fake claude: %v", err)
	}

	t.Setenv("MULTICA_CLAUDE_PATH", fakeClaude)
	t.Setenv("MULTICA_CLAUDE_API_KEY", "")
	t.Setenv("MULTICA_CLAUDE_BASE_URL", "")
	for _, v := range []string{
		"MULTICA_CODEX_PATH", "MULTICA_OPENCODE_PATH",
		"MULTICA_OPENCLAW_PATH", "MULTICA_HERMES_PATH",
	} {
		t.Setenv(v, "/nonexistent/binary-"+v)
	}

	cfg, err := LoadConfig(Overrides{})
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	entry := cfg.Agents["claude"]
	if len(entry.ExtraEnv) != 0 {
		t.Errorf("expected empty ExtraEnv, got %v", entry.ExtraEnv)
	}
}
