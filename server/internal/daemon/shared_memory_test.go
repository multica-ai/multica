package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPromptInjectsRelevantSharedMemoryFromIssueDescription(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "facts"), 0o755); err != nil {
		t.Fatal(err)
	}
	fact := "---\nname: test-fact\ndescription: 해외 ETP 신청 상태\n---\n2026-11-17 해외ETP 거래신청 완료.\n"
	if err := os.WriteFile(filepath.Join(root, "facts", "test-fact.md"), []byte(fact), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_MEMORY_ROOT", root)
	t.Setenv("MULTICA_MEMORY_SEARCH_SCRIPT", filepath.Join(root, "missing-search.py"))
	t.Setenv("MULTICA_SHARED_MEMORY", "1")

	prompt := BuildPrompt(Task{IssueID: "issue-1", IssueTitle: "ETP application", IssueDescription: "Should I ask the CEO to log in and apply for overseas ETP?"}, "codex")
	if !strings.Contains(prompt, "Relevant shared memory") || !strings.Contains(prompt, "test-fact.md") || !strings.Contains(prompt, "거래신청 완료") {
		t.Fatalf("expected relevant memory injected into common prompt, got:\n%s", prompt)
	}
}

func TestSharedMemoryPromptBoundsCountAndApproximateTokenBudget(t *testing.T) {
	facts := make([]sharedMemoryFact, 12)
	for i := range facts {
		facts[i] = sharedMemoryFact{Path: "fact.md", Content: strings.Repeat("가", 10000)}
	}
	got := renderSharedMemoryContext(facts)
	if strings.Count(got, "source:") > sharedMemoryTopN {
		t.Fatalf("rendered more than top-N=%d facts", sharedMemoryTopN)
	}
	if len(got) > sharedMemoryMaxTokens*4 {
		t.Fatalf("rendered context exceeds token budget: chars=%d budget=%d", len(got), sharedMemoryMaxTokens*4)
	}
}

func TestSharedMemoryRollbackFlagDisablesInjection(t *testing.T) {
	t.Setenv("MULTICA_SHARED_MEMORY", "0")
	if got := BuildSharedMemoryContext(Task{ThreadName: "overseas ETP"}); got != "" {
		t.Fatalf("rollback flag should disable retrieval, got %q", got)
	}
}
