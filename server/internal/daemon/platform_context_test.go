package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

func TestStripPlatformBuiltinSkillsPreservesWorkspaceSkills(t *testing.T) {
	task := Task{Agent: &AgentData{
		SkillRefs: []SkillRefData{
			{ID: "builtin:one", Source: skillbundle.SourceBuiltin, Name: "one"},
			{ID: "builtin:legacy", Name: "legacy"},
			{ID: "workspace-id", Source: skillbundle.SourceWorkspace, Name: "workspace"},
		},
		Skills: []SkillData{
			{ID: "builtin:one", Source: skillbundle.SourceBuiltin, Name: "one"},
			{ID: "builtin:legacy", Name: "legacy"},
			{ID: "workspace-id", Source: skillbundle.SourceWorkspace, Name: "workspace"},
		},
	}}
	originalAgent := task.Agent

	stripPlatformBuiltinSkills(&task)

	if got := task.Agent.SkillRefs; len(got) != 1 || got[0].ID != "workspace-id" {
		t.Fatalf("SkillRefs after filtering = %#v, want only workspace skill", got)
	}
	if got := task.Agent.Skills; len(got) != 1 || got[0].ID != "workspace-id" {
		t.Fatalf("Skills after filtering = %#v, want only workspace skill", got)
	}
	if len(originalAgent.SkillRefs) != 3 || len(originalAgent.Skills) != 3 {
		t.Fatal("filtering mutated the claimed task's shared AgentData")
	}
}

func TestSyncPlatformRuntimeBriefModesReplaceAndCleanManagedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	const userContent = "# Repository instructions\n\nKeep this content byte-for-byte.\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	brief, err := syncPlatformRuntimeBrief(dir, "codex", execenv.TaskContextForEnv{
		ChatSessionID: "chat-1",
		AgentName:     "test-agent",
		AgentSkills: []execenv.SkillContextForEnv{{
			Name:        "multica-example",
			Description: "Use when operating an example Multica resource.",
		}},
	}, cli.PlatformContextFull)
	if err != nil {
		t.Fatalf("full platform context: %v", err)
	}
	if brief == "" {
		t.Fatal("enabled platform context returned an empty brief")
	}
	injected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(injected), "BEGIN MULTICA-RUNTIME") {
		t.Fatalf("enabled platform context did not inject managed block:\n%s", injected)
	}
	if !strings.Contains(string(injected), "## Important: Always Use the `multica` CLI") {
		t.Fatalf("full platform context is missing historical workflow rules:\n%s", injected)
	}

	brief, err = syncPlatformRuntimeBrief(dir, "codex", execenv.TaskContextForEnv{
		AgentSkills: []execenv.SkillContextForEnv{{
			Name:        "multica-example",
			Description: "Use when operating an example Multica resource.",
		}},
	}, cli.PlatformContextMinimal)
	if err != nil {
		t.Fatalf("minimal platform context: %v", err)
	}
	if !strings.Contains(brief, "## Available Task Skills") || !strings.Contains(brief, "multica-example") {
		t.Fatalf("minimal context is missing the skill catalog:\n%s", brief)
	}
	if strings.Contains(brief, "Always Use") || strings.Contains(brief, "## Workflow") {
		t.Fatalf("minimal context contains forced workflow rules:\n%s", brief)
	}
	minimal, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(minimal), "BEGIN MULTICA-RUNTIME") != 1 || !strings.Contains(string(minimal), "## Available Task Skills") {
		t.Fatalf("minimal context did not replace the managed block:\n%s", minimal)
	}

	brief, err = syncPlatformRuntimeBrief(dir, "codex", execenv.TaskContextForEnv{}, cli.PlatformContextOff)
	if err != nil {
		t.Fatalf("off platform context: %v", err)
	}
	if brief != "" {
		t.Fatalf("disabled platform context returned brief %q, want empty", brief)
	}
	cleaned, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(cleaned) != userContent {
		t.Fatalf("cleanup changed repository instructions:\n got %q\nwant %q", cleaned, userContent)
	}
}
