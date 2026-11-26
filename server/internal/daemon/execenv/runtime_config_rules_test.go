package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// longAntigravityContext pads the agent instructions so the brief is well past
// agy's 24,000-byte per-file cap, with multi-byte text and a canary at the end
// of the user-provided part.
func longAntigravityContext() TaskContextForEnv {
	instructions := strings.Repeat("团队说明：处理 GitHub issue 时先读完全部评论，再给出结论。\n", 600) + "INSTRUCTIONS-TAIL-CANARY\n"
	return TaskContextForEnv{IssueID: "issue-1", TriggerCommentID: "comment-1", AgentInstructions: instructions}
}

func TestAntigravityRuleFilesCarryLongBriefUnderCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	brief, err := InjectRuntimeConfig(dir, "antigravity", longAntigravityContext())
	if err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	if len(brief) < 2*24000 {
		t.Fatalf("fixture brief is only %d bytes; it must span several rule files", len(brief))
	}
	paths, _ := filepath.Glob(filepath.Join(antigravityRulesDir(dir), antigravityRuleFilePrefix+"*.md"))
	if len(paths) < 3 {
		t.Fatalf("expected the brief split across several rule files, got %d", len(paths))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 24000 {
			t.Errorf("%s is %d bytes, over agy's 24,000-byte rule file cap", filepath.Base(path), len(data))
		}
		if !utf8.Valid(data) {
			t.Errorf("%s is not valid UTF-8", filepath.Base(path))
		}
	}
	got := readAntigravityRuleFiles(t, dir)
	if strings.TrimRight(got, "\n") != strings.TrimRight(brief, "\n") {
		t.Fatal("rule files together must carry the brief verbatim")
	}
	for _, want := range []string{"INSTRUCTIONS-TAIL-CANARY", "## Mentions", "## Output"} {
		if !strings.Contains(got, want) {
			t.Errorf("rule files lost %q", want)
		}
	}
}

func TestSplitBriefForRuleFilesKeepsSectionsWhole(t *testing.T) {
	t.Parallel()

	sectionA := "## A\n" + strings.Repeat("a", 50) + "\n\n"
	sectionB := "## B\n" + strings.Repeat("b", 50) + "\n\n"
	sectionC := "### C\n" + strings.Repeat("c", 50) + "\n"
	parts := splitBriefForRuleFiles(sectionA+sectionB+sectionC, 120)
	want := []string{sectionA + sectionB, sectionC}
	if len(parts) != len(want) {
		t.Fatalf("parts = %q, want %q", parts, want)
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Errorf("part %d = %q, want %q", i, parts[i], want[i])
		}
	}

	// A heading inside a code fence is not a section break.
	fenced := "## A\n```\n## not a heading\n```\n"
	if got := splitBriefSections(fenced); len(got) != 1 {
		t.Errorf("fenced heading split the section: %q", got)
	}

	// A section, and even a line, over the limit is cut without breaking runes.
	long := "## Big\n" + strings.Repeat("界", 100) + "\n"
	parts = splitBriefForRuleFiles(long, 40)
	if strings.Join(parts, "") != long {
		t.Fatalf("oversized section lost bytes: %q", parts)
	}
	for _, p := range parts {
		if len(p) > 40 || !utf8.ValidString(p) {
			t.Errorf("bad part %q (%d bytes)", p, len(p))
		}
	}
}

// Re-injection replaces the parts without leaving stale ones; cleanup removes
// only the daemon's files and the dirs it emptied.
func TestAntigravityRuleFilesReinjectAndCleanup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if _, err := InjectRuntimeConfig(dir, "antigravity", longAntigravityContext()); err != nil {
		t.Fatal(err)
	}
	brief, err := InjectRuntimeConfig(dir, "antigravity", TaskContextForEnv{IssueID: "issue-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := readAntigravityRuleFiles(t, dir); strings.TrimRight(got, "\n") != strings.TrimRight(brief, "\n") {
		t.Fatal("re-injected rule files still carry parts of the previous brief")
	}

	if err := CleanupRuntimeConfig(dir, "antigravity"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("cleanup must remove the dirs it emptied, stat err = %v", err)
	}

	// A user's own rule, even one sharing the prefix, survives cleanup.
	userRule := filepath.Join(antigravityRulesDir(dir), antigravityRuleFilePrefix+"notes.md")
	if err := os.MkdirAll(filepath.Dir(userRule), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userRule, []byte("---\ntrigger: always_on\n---\nmine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InjectRuntimeConfig(dir, "antigravity", TaskContextForEnv{IssueID: "issue-1"}); err != nil {
		t.Fatal(err)
	}
	if err := CleanupRuntimeConfig(dir, "antigravity"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(userRule); err != nil || string(data) != "---\ntrigger: always_on\n---\nmine\n" {
		t.Fatalf("user rule changed: %q, %v", data, err)
	}
}

// The repository AGENTS.md is left alone, and a brief block an older daemon
// appended to it is removed so agy does not load a stale, truncated copy.
func TestAntigravityLeavesRepositoryAgentsMD(t *testing.T) {
	t.Parallel()

	const userContent = "# Repository instructions\n\nkeep me\n"
	for name, seed := range map[string]string{
		"untouched":   userContent,
		"stale block": userContent + runtimeManagedSeparator + runtimeMarkerBegin + "\nold brief\n" + runtimeMarkerEnd + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "AGENTS.md")
			if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := InjectRuntimeConfig(dir, "antigravity", TaskContextForEnv{IssueID: "issue-1"}); err != nil {
				t.Fatalf("InjectRuntimeConfig: %v", err)
			}
			if got, _ := os.ReadFile(path); string(got) != userContent {
				t.Fatalf("AGENTS.md = %q, want repository content %q", got, userContent)
			}
		})
	}
}
