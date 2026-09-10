package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
)

func lifecycleFileTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("allow-external-file", false, "")
	return cmd
}

func writeLifecycleTestFile(t *testing.T, body string) string {
	t.Helper()
	file, err := os.CreateTemp(".", "lifecycle-*.yml")
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

func TestReadLifecycleFileStrictYAML(t *testing.T) {
	path := writeLifecycleTestFile(t, `api_version: 1
name: SDLC
initial_status: spec
statuses:
  - key: spec
    name: Technical Spec
    color: "#8b5cf6"
    phase: unstarted
`)
	spec, err := readLifecycleFile(lifecycleFileTestCommand(t), path, "file")
	if err != nil {
		t.Fatal(err)
	}
	if spec.APIVersion != 1 || spec.InitialStatus != "spec" || len(spec.Statuses) != 1 {
		t.Fatalf("decoded spec = %#v", spec)
	}

	badPath := writeLifecycleTestFile(t, "api_version: 1\nunknown_field: true\n")
	if _, err := readLifecycleFile(lifecycleFileTestCommand(t), badPath, "file"); err == nil || !strings.Contains(err.Error(), "field unknown_field not found") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestResolveLifecyclePrincipalDefaultsWithoutNetwork(t *testing.T) {
	client := &cli.APIClient{}
	assignee, err := resolveLifecyclePrincipal(context.Background(), client, lifecycleFilePrincipal{}, false)
	if err != nil || assignee.Type != issuelifecycle.AssigneeKeep {
		t.Fatalf("default assignee = %#v, %v", assignee, err)
	}
	executor, err := resolveLifecyclePrincipal(context.Background(), client, lifecycleFilePrincipal{}, true)
	if err != nil || executor.Type != issuelifecycle.ExecutorNone {
		t.Fatalf("default executor = %#v, %v", executor, err)
	}
}
