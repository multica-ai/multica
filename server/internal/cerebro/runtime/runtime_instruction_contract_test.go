package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// TestPermissionLookupInstructionReachesEverySupportedRuntime keeps the
// canonical access lookup in every file-backed runtime brief. This is a
// cerebro-side contract test so the shared daemon instruction writer remains
// upstream-owned while an added runtime cannot silently omit the instruction.
func TestPermissionLookupInstructionReachesEverySupportedRuntime(t *testing.T) {
	for _, provider := range []string{
		"claude", "codex", "copilot", "opencode", "openclaw", "hermes",
		"pi", "cursor", "kimi", "kiro", "antigravity", "gemini",
	} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := execenv.InjectRuntimeConfig(dir, provider, execenv.TaskContextForEnv{IssueID: "issue-1"}); err != nil {
				t.Fatalf("inject runtime config: %v", err)
			}

			fileName := "AGENTS.md"
			if provider == "claude" {
				fileName = "CLAUDE.md"
			} else if provider == "gemini" {
				fileName = "GEMINI.md"
			}
			content, err := os.ReadFile(filepath.Join(dir, fileName))
			if err != nil {
				t.Fatalf("read runtime brief: %v", err)
			}
			if !strings.Contains(string(content), "For every access question, use `get_agent_capabilities` as the canonical lookup") {
				t.Fatalf("runtime brief must direct access questions to get_agent_capabilities")
			}
		})
	}
}
