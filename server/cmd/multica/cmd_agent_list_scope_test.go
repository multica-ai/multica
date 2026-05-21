package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// freshAgentListCmd mirrors agentListCmd's flag wiring for use in tests so
// each subtest gets a clean flag state. Keeps the production command's
// cobra.Command immutable across runs.
func freshAgentListCmd() *cobra.Command {
	c := &cobra.Command{Use: "list"}
	c.Flags().String("output", "json", "Output format")
	c.Flags().Bool("include-archived", false, "Include archived agents")
	c.Flags().Bool("all", false, "Return the full workspace agent list even inside a squad-leader task")
	c.PersistentFlags().String("profile", "", "")
	return c
}

// captureListAgentsQuery starts a stub server that records the
// /api/agents request's RawQuery and returns an empty agent list. Caller
// gets back the captured query and the server's URL.
func captureListAgentsQuery(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents" {
			http.NotFound(w, r)
			return
		}
		captured = r.URL.RawQuery
		_, _ = io.WriteString(w, "[]")
	}))
	return srv, &captured
}

func TestRunAgentList_AddsTaskSquadScopeInAgentContext(t *testing.T) {
	srv, captured := captureListAgentsQuery(t)
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "agent-123")
	t.Setenv("MULTICA_TASK_ID", "task-456")

	cmd := freshAgentListCmd()
	if err := runAgentList(cmd, nil); err != nil {
		t.Fatalf("runAgentList: %v", err)
	}

	if !strings.Contains(*captured, "scope=task_squad") {
		t.Fatalf("expected query to include scope=task_squad inside agent context, got %q", *captured)
	}
}

func TestRunAgentList_OmitsTaskSquadScopeWithAllFlag(t *testing.T) {
	srv, captured := captureListAgentsQuery(t)
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "agent-123")
	t.Setenv("MULTICA_TASK_ID", "task-456")

	cmd := freshAgentListCmd()
	if err := cmd.Flags().Set("all", "true"); err != nil {
		t.Fatalf("set --all: %v", err)
	}
	if err := runAgentList(cmd, nil); err != nil {
		t.Fatalf("runAgentList: %v", err)
	}

	if strings.Contains(*captured, "scope=task_squad") {
		t.Fatalf("expected --all to suppress scope param, got %q", *captured)
	}
}

func TestRunAgentList_OmitsTaskSquadScopeOutsideAgentContext(t *testing.T) {
	srv, captured := captureListAgentsQuery(t)
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")

	cmd := freshAgentListCmd()
	if err := runAgentList(cmd, nil); err != nil {
		t.Fatalf("runAgentList: %v", err)
	}

	if strings.Contains(*captured, "scope=task_squad") {
		t.Fatalf("expected no scope param outside agent context, got %q", *captured)
	}
}
