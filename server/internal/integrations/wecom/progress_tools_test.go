package wecom

// progress_tools_test.go — the tool name table, checked against what the
// providers actually emit.
//
// A name this adapter has never seen still produces a line, which is the right
// failure mode. But it produces "正在使用 list_directory" — the provider's
// identifier, in English, in the middle of Chinese copy — and a run on a
// provider whose whole vocabulary is missing reads as a wall of them. So the
// table is the feature, and this file is the inventory it is checked against:
// every name below was read off the provider's own emitter or its test
// fixtures, not guessed.

import (
	"strings"
	"testing"
)

// toolKinds is the inventory, by the provider that emits each name.
//
// Claude Code and CodeBuddy pass the tool's own name through (claude.go).
// Codex hardcodes two (codex.go). Cursor strips the suffix off
// "<name>ToolCall" (cursor.go). The ACP family — Qoder, Kimi, Kiro, Hermes —
// normalises the ACP title to snake_case and passes anything it does not
// recognise through as snake_case, which is how the Gemini-CLI vocabulary
// qwen-code inherits shows up (hermes.go, kimi.go, kiro.go).
var toolKinds = map[string]map[string]progressKind{
	"claude code": {
		"Read":          progressRead,
		"NotebookRead":  progressRead,
		"Write":         progressEdit,
		"Edit":          progressEdit,
		"MultiEdit":     progressEdit,
		"NotebookEdit":  progressEdit,
		"Bash":          progressCommand,
		"BashOutput":    progressCommand,
		"KillShell":     progressCommand,
		"SlashCommand":  progressCommand,
		"Glob":          progressSearch,
		"Grep":          progressSearch,
		"WebFetch":      progressWeb,
		"WebSearch":     progressWeb,
		"Task":          progressSubtask,
		"Skill":         progressSkill,
		"TodoWrite":     progressPlan,
		"ExitPlanMode":  progressPlan,
		"EnterPlanMode": progressPlan,
	},
	"codex": {
		"exec_command": progressCommand,
		"patch_apply":  progressEdit,
		"update_plan":  progressPlan,
	},
	"cursor": {
		"read":        progressRead,
		"write":       progressEdit,
		"edit":        progressEdit,
		"delete":      progressEdit,
		"shell":       progressCommand,
		"grep":        progressSearch,
		"glob":        progressSearch,
		"ls":          progressSearch,
		"search":      progressSearch,
		"updateTodos": progressPlan,
	},
	"acp family": {
		"read_file":      progressRead,
		"vision_analyze": progressRead,
		"write_file":     progressEdit,
		"edit_file":      progressEdit,
		"patch":          progressEdit,
		"code":           progressEdit,
		"terminal":       progressCommand,
		"execute_code":   progressCommand,
		"search_files":   progressSearch,
		"web_search":     progressWeb,
		"web_fetch":      progressWeb,
		"web_extract":    progressWeb,
		"delegate_task":  progressSubtask,
		"todo_write":     progressPlan,
		"thinking":       progressPlan,
	},
	"gemini cli vocabulary (qwen)": {
		"list_directory":      progressSearch,
		"search_file_content": progressSearch,
		"read_many_files":     progressRead,
		"replace":             progressEdit,
		"run_shell_command":   progressCommand,
		"google_web_search":   progressWeb,
	},
}

// TestEveryProvidersToolNamesAreMapped — the inventory, asserted as the kind
// each name resolves to. A name that resolves to progressTool is one that
// reads as "正在使用 <identifier>", which is the thing this table exists to
// avoid.
func TestEveryProvidersToolNamesAreMapped(t *testing.T) {
	for provider, names := range toolKinds {
		t.Run(provider, func(t *testing.T) {
			for name, want := range names {
				step := stepFromToolUse(name, nil)
				if step.kind == progressTool {
					t.Errorf("%s fell back to the unknown-tool line", name)
					continue
				}
				if step.kind != want {
					t.Errorf("%s classified as kind %d, want %d", name, step.kind, want)
				}
			}
		})
	}
}

// TestToolNamesMatchWhateverCaseTheyArriveIn — Claude Code sends TitleCase,
// the ACP family snake_case, Cursor camelCase, and the same tool can arrive
// either way from a workspace running two providers.
func TestToolNamesMatchWhateverCaseTheyArriveIn(t *testing.T) {
	for _, name := range []string{"ExitPlanMode", "exitplanmode", "EXITPLANMODE", "  ExitPlanMode  "} {
		if got := stepFromToolUse(name, nil).kind; got != progressPlan {
			t.Errorf("%q classified as kind %d, want the plan line", name, got)
		}
	}
}

// TestAGenuinelyUnknownToolStillSaysSomething — the table is not a whitelist.
// A tool nobody has taught this adapter still produces a line, because a step
// the user never sees happen is indistinguishable from a run that stalled.
func TestAGenuinelyUnknownToolStillSaysSomething(t *testing.T) {
	line := stepFromToolUse("Frobnicate", map[string]any{"target": "ledger"}).line(copyFor(LocaleZhHans), progressLevelDetail)
	if !strings.Contains(line, "Frobnicate") || !strings.Contains(line, "ledger") {
		t.Errorf("line = %q, want the tool's name and what it was given", line)
	}
}

// TestASkillReadsAsASkill — Skill is neither reading, editing, running nor
// searching, and "正在使用 Skill：skill=pdf" says the tool's name where the
// skill's name is what matters.
func TestASkillReadsAsASkill(t *testing.T) {
	line := stepFromToolUse("Skill", map[string]any{"skill": "pdf-tools"}).line(copyFor(LocaleZhHans), progressLevelDetail)
	if !strings.Contains(line, "pdf-tools") {
		t.Errorf("line = %q, want the skill named", line)
	}
	if strings.Contains(line, "skill=") {
		t.Errorf("line = %q reads as a parameter dump", line)
	}
}
