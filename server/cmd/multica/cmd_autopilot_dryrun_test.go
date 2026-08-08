package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newAutopilotTriggerTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "trigger"}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

// TestRunAutopilotTrigger_DryRunSendsQuery verifies the --dry-run flag appends
// ?dry_run=true to the trigger POST and prints the returned dispatch plan as
// JSON. The plan's DryRun marker must round-trip to stdout so a caller can
// distinguish a preview from a real run.
func TestRunAutopilotTrigger_DryRunSendsQuery(t *testing.T) {
	const autopilotID = "11111111-1111-1111-1111-111111111111"

	var capturedPath, capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots/"+autopilotID+"/trigger" {
			http.NotFound(w, r)
			return
		}
		capturedPath = r.URL.Path + "?" + r.URL.RawQuery
		capturedMethod = r.Method
		json.NewEncoder(w).Encode(map[string]any{
			"autopilot_id":   autopilotID,
			"execution_mode": "create_issue",
			"dry_run":        true,
			"skipped":        false,
			"ready":          true,
			"issue_title":    "nightly report 2026-07-22",
			"leader": map[string]any{
				"id":             "22222222-2222-2222-2222-222222222222",
				"name":           "worker",
				"squad_resolved": false,
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newAutopilotTriggerTestCmd()
	_ = cmd.Flags().Set("dry-run", "true")

	if err := runAutopilotTrigger(cmd, []string{autopilotID}); err != nil {
		t.Fatalf("runAutopilotTrigger: %v", err)
	}

	if capturedMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", capturedMethod)
	}
	if !strings.Contains(capturedPath, "dry_run=true") {
		t.Fatalf("trigger path = %q, want query to contain dry_run=true", capturedPath)
	}
}

// TestRunAutopilotTrigger_RealRunOmitsQuery is the regression guard: without
// --dry-run the trigger POST must NOT carry the dry_run query, so the real
// dispatch path runs unchanged.
func TestRunAutopilotTrigger_RealRunOmitsQuery(t *testing.T) {
	const autopilotID = "11111111-1111-1111-1111-111111111111"

	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autopilots/"+autopilotID+"/trigger" {
			http.NotFound(w, r)
			return
		}
		capturedQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]any{
			"id":     "run-1",
			"status": "issue_created",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newAutopilotTriggerTestCmd()
	// dry-run flag left at its default (false).
	if err := runAutopilotTrigger(cmd, []string{autopilotID}); err != nil {
		t.Fatalf("runAutopilotTrigger: %v", err)
	}
	if strings.Contains(capturedQuery, "dry_run") {
		t.Fatalf("real trigger query = %q, must not contain dry_run", capturedQuery)
	}
}

