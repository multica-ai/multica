package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/codexcontext"
)

func TestResolveCodexTaskContext(t *testing.T) {
	t.Parallel()

	t.Run("defaults to inherited", func(t *testing.T) {
		mode, operational, err := resolveCodexTaskContext(Task{}, "codex")
		if err != nil {
			t.Fatalf("resolveCodexTaskContext() error = %v", err)
		}
		if mode != codexcontext.ModeInherited || operational != nil {
			t.Fatalf("got mode=%q operational=%#v", mode, operational)
		}
	})

	t.Run("builds operational context once", func(t *testing.T) {
		task := Task{
			IssueID: "issue-123",
			Agent: &AgentData{
				Instructions:  "Act as an operational scout.",
				RuntimeConfig: json.RawMessage(`{"codex":{"context_mode":"operational"}}`),
				Skills: []SkillData{
					{Name: "Runner Health", Content: "Inspect runner health.", Files: []SkillFileData{{Path: "check.sh", Content: "echo ok"}}},
				},
			},
		}
		mode, operational, err := resolveCodexTaskContext(task, "codex")
		if err != nil {
			t.Fatalf("resolveCodexTaskContext() error = %v", err)
		}
		if mode != codexcontext.ModeOperational || operational == nil {
			t.Fatalf("got mode=%q operational=%#v", mode, operational)
		}
		if !strings.Contains(operational.Prompt, "issue-123") || len(operational.Skills) != 1 || len(operational.Skills[0].Files) != 1 {
			t.Fatalf("operational context lost task or skill data: %#v", operational)
		}
	})

	t.Run("rejects operational mode for another provider", func(t *testing.T) {
		_, _, err := resolveCodexTaskContext(Task{Agent: &AgentData{
			RuntimeConfig: json.RawMessage(`{"codex":{"context_mode":"operational"}}`),
		}}, "claude")
		if err == nil {
			t.Fatal("resolveCodexTaskContext() error = nil")
		}
	})

	t.Run("rejects invalid explicit mode", func(t *testing.T) {
		_, _, err := resolveCodexTaskContext(Task{Agent: &AgentData{
			RuntimeConfig: json.RawMessage(`{"codex":{"context_mode":"unknown"}}`),
		}}, "codex")
		if err == nil {
			t.Fatal("resolveCodexTaskContext() error = nil")
		}
	})
}

func TestApplyOperationalExecOptions(t *testing.T) {
	t.Parallel()

	context := &codexcontext.OperationalContext{
		BaseInstructions:      "base",
		DeveloperInstructions: "developer",
	}
	opts := agent.ExecOptions{
		ResumeSessionID:        "prior",
		ResumeExpected:         true,
		ResumeContinuityNotice: "notice",
		ExtraArgs:              []string{"--extra"},
		CustomArgs:             []string{"--custom"},
		McpConfig:              json.RawMessage(`{"servers":{"ambient":{}}}`),
	}

	applyOperationalExecOptions(&opts, context)
	if opts.CodexContextMode != codexcontext.ModeOperational {
		t.Fatalf("CodexContextMode = %q", opts.CodexContextMode)
	}
	if opts.BaseInstructions != "base" || opts.DeveloperInstructions != "developer" {
		t.Fatalf("instructions not applied: %#v", opts)
	}
	if opts.ResumeSessionID != "" || opts.ResumeExpected || opts.ResumeContinuityNotice != "" {
		t.Fatalf("resume state remains: %#v", opts)
	}
	if len(opts.ExtraArgs) != 0 || len(opts.CustomArgs) != 0 || len(opts.McpConfig) != 0 {
		t.Fatalf("ambient invocation configuration remains: %#v", opts)
	}
}
