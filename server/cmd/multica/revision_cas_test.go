package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func TestAgentUpdatePromptUsesProvidedSnapshotRevision(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "agent-1", "revision": 8})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	// A task-scoped mat_ token so the test also passes inside an agent workdir,
	// where a daemon task marker makes newAPIClient reject a plain token.
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("instructions", "", "")
	cmd.Flags().Int64("expected-revision", 0, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	_ = cmd.Flags().Set("instructions", "edit from snapshot 7")
	_ = cmd.Flags().Set("expected-revision", "7")

	if err := runAgentUpdate(cmd, []string{"agent-1"}); err != nil {
		t.Fatalf("runAgentUpdate: %v", err)
	}
	if body["instructions"] != "edit from snapshot 7" || body["expected_revision"] != float64(7) {
		t.Fatalf("prompt update body = %#v, want immutable text snapshot and revision 7", body)
	}
}

func TestAutopilotUpdatePromptUsesProvidedSnapshotRevision(t *testing.T) {
	const autopilotID = "11111111-1111-1111-1111-111111111111"
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": autopilotID, "revision": 8})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	// A task-scoped mat_ token so the test also passes inside an agent workdir,
	// where a daemon task marker makes newAPIClient reject a plain token.
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newAutopilotUpdateTestCmd()
	cmd.Flags().Int64("expected-revision", 0, "")
	_ = cmd.Flags().Set("description", "edit from snapshot 7")
	_ = cmd.Flags().Set("expected-revision", "7")

	if err := runAutopilotUpdate(cmd, []string{autopilotID}); err != nil {
		t.Fatalf("runAutopilotUpdate: %v", err)
	}
	if body["description"] != "edit from snapshot 7" || body["expected_revision"] != float64(7) {
		t.Fatalf("prompt update body = %#v, want immutable text snapshot and revision 7", body)
	}
}
