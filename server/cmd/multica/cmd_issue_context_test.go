package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func freshIssueContextCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "context"}
	cmd.Flags().String("output", "json", "")
	cmd.PersistentFlags().String("profile", "", "")
	return cmd
}

func TestRunIssueContext_PrintsRunContextJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "context.json")
	want := map[string]any{
		"task": map[string]any{
			"id":           "task-1",
			"kind":         "direct",
			"attempt":      float64(1),
			"max_attempts": float64(2),
		},
		"issue": map[string]any{
			"id":         "issue-1",
			"identifier": "MUL-1",
			"title":      "Run context",
			"status":     "in_progress",
			"priority":   "high",
		},
		"parent":     nil,
		"properties": map[string]any{},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("MULTICA_RUN_CONTEXT", path)
	t.Setenv("MULTICA_AGENT_ID", "11111111-1111-4111-8111-111111111111")
	t.Setenv("MULTICA_TASK_ID", "22222222-2222-4222-8222-222222222222")

	cmd := freshIssueContextCmd()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runIssueContext(cmd, nil)

	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)

	if err != nil {
		t.Fatalf("runIssueContext: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("stdout is not json: %v\n%s", err, string(out))
	}
	task, _ := got["task"].(map[string]any)
	if task["id"] != "task-1" || task["kind"] != "direct" {
		t.Fatalf("unexpected task payload: %+v", task)
	}
}

func TestRunIssueContext_MissingEnvErrorsInAgentContext(t *testing.T) {
	t.Setenv("MULTICA_RUN_CONTEXT", "")
	t.Setenv("MULTICA_AGENT_ID", "11111111-1111-4111-8111-111111111111")
	t.Setenv("MULTICA_TASK_ID", "22222222-2222-4222-8222-222222222222")

	err := runIssueContext(freshIssueContextCmd(), nil)
	if err == nil {
		t.Fatal("expected error when MULTICA_RUN_CONTEXT is missing")
	}
	if !strings.Contains(err.Error(), "MULTICA_RUN_CONTEXT") {
		t.Fatalf("error should mention MULTICA_RUN_CONTEXT, got %q", err.Error())
	}
}

func TestRunIssueContext_MissingFileMentionsOrphanedRun(t *testing.T) {
	t.Setenv("MULTICA_RUN_CONTEXT", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("MULTICA_AGENT_ID", "11111111-1111-4111-8111-111111111111")
	t.Setenv("MULTICA_TASK_ID", "22222222-2222-4222-8222-222222222222")

	err := runIssueContext(freshIssueContextCmd(), nil)
	if err == nil {
		t.Fatal("expected error when run context file is missing")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "orphaned") {
		t.Fatalf("error should mention orphaned run cleanup risk, got %q", err.Error())
	}
}
