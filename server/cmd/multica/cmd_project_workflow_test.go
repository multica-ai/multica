package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
)

func workflowFileTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("allow-external-file", false, "")
	return cmd
}

func writeWorkflowTestFile(t *testing.T, body string) string {
	t.Helper()
	file, err := os.CreateTemp(".", "workflow-*.yml")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	t.Cleanup(func() { _ = os.Remove(path) })
	if _, err := file.WriteString(body); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadWorkflowFileStrictYAML(t *testing.T) {
	path := writeWorkflowTestFile(t, `api_version: 1
name: SDLC
initial_status: spec
statuses:
  - key: spec
    name: Technical Spec
    color: "#8b5cf6"
    phase: unstarted
    entry_policy:
      next_status_key: review
`)
	spec, err := readWorkflowFile(workflowFileTestCommand(t), path, "file")
	if err != nil {
		t.Fatal(err)
	}
	if spec.APIVersion != 1 || spec.InitialStatus != "spec" || len(spec.Statuses) != 1 {
		t.Fatalf("decoded spec = %#v", spec)
	}

	resolved, err := resolveWorkflowFileSpec(context.Background(), &cli.APIClient{}, spec)
	if err != nil || resolved.Statuses[0].EntryPolicy.NextStatusKey != "review" {
		t.Fatalf("handoff destination lost during CLI resolution: %#v, %v", resolved, err)
	}

	badPath := writeWorkflowTestFile(t, "api_version: 1\nunknown_field: true\n")
	if _, err := readWorkflowFile(workflowFileTestCommand(t), badPath, "file"); err == nil || !strings.Contains(err.Error(), "field unknown_field not found") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestResolveWorkflowPrincipalDefaultsWithoutNetwork(t *testing.T) {
	client := &cli.APIClient{}
	assignee, err := resolveWorkflowPrincipal(context.Background(), client, workflowFilePrincipal{}, false)
	if err != nil || assignee.Type != issueworkflow.AssigneeKeep {
		t.Fatalf("default assignee = %#v, %v", assignee, err)
	}
	executor, err := resolveWorkflowPrincipal(context.Background(), client, workflowFilePrincipal{}, true)
	if err != nil || executor.Type != issueworkflow.ExecutorNone {
		t.Fatalf("default executor = %#v, %v", executor, err)
	}
}
