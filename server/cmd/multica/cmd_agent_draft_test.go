package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunAgentDraftWritesStructuredOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai-task-output.json")
	t.Setenv(aiTaskOutputPathEnv, path)
	cmd := freshAgentDraftCmdForTest()
	cmd.SetArgs([]string{"--output-results", `{"agent_id":"00000000-0000-0000-0000-000000000123","name":"A","summary":"B","skill_source_urls":[]}`})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("agent draft failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) == "" {
		t.Fatal("expected output file to be written")
	}
}

func freshAgentDraftCmdForTest() *cobra.Command {
	cmd := &cobra.Command{Use: "draft", RunE: runAgentDraft}
	cmd.Flags().String("output-results", "", "Structured JSON agent draft result for AI task capture")
	return cmd
}
