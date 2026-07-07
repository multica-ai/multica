// CEREBRO-PATCH(cerebro-sprints-cli): FIR-2500 cerebro-only file — tests for
// the sprint CLI against the real sprint feature endpoints.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const (
	testSprintUUID      = "40003764-b62b-4052-a6a4-40f1e6b13020"
	testSprintIssueUUID = "22222222-2222-2222-2222-222222222222"
	testProjectUUID     = "33333333-3333-3333-3333-333333333333"
)

func testSprintObject() map[string]any {
	return map[string]any{
		"id":            testSprintUUID,
		"workspace_id":  "ws-1",
		"project_id":    testProjectUUID,
		"project_title": "Roadmap",
		"name":          "Sprint 12",
		"sequence_no":   12,
		"status":        "active",
		"start_date":    "2026-07-06",
		"end_date":      "2026-07-19",
	}
}

// sprintTestServer wires a fake backend covering the endpoints the sprint CLI
// talks to. Handlers may be nil; unmatched paths 404. Captures request paths
// so tests can assert routing.
func sprintTestServer(t *testing.T, handler http.HandlerFunc) *[]string {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return &paths
}

func newSprintTestCmd(flags map[string]string) *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.Flags().String("status", "", "")
	c.Flags().String("output", "json", "")
	c.Flags().Bool("full-id", false, "")
	c.Flags().String("name", "", "")
	c.Flags().String("start", "", "")
	c.Flags().String("end", "", "")
	c.Flags().String("goal", "", "")
	c.Flags().String("sprint", "", "")
	for k, v := range flags {
		if err := c.Flags().Set(k, v); err != nil {
			panic(err)
		}
	}
	return c
}

func TestRunSprintListNoArgListsWorkspaceSprints(t *testing.T) {
	paths := sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/sprints" {
			_ = json.NewEncoder(w).Encode(map[string]any{"sprints": []any{testSprintObject()}})
			return
		}
		http.NotFound(w, r)
	})

	cmd := newSprintTestCmd(map[string]string{"status": "active"})
	out, err := captureStdout(t, func() error {
		return runSprintList(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runSprintList: %v", err)
	}
	if !strings.Contains(out, "Sprint 12") || !strings.Contains(out, testSprintUUID) {
		t.Fatalf("output missing sprint data: %s", out)
	}
	if len(*paths) != 1 || (*paths)[0] != "GET /api/cerebro/sprints?status=active" {
		t.Fatalf("unexpected routing: %v", *paths)
	}
}

func TestRunSprintGetByUUIDReturnsSprint(t *testing.T) {
	_ = sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/sprints/"+testSprintUUID {
			_ = json.NewEncoder(w).Encode(testSprintObject())
			return
		}
		http.NotFound(w, r)
	})

	cmd := newSprintTestCmd(nil)
	out, err := captureStdout(t, func() error {
		return runSprintGet(cmd, []string{testSprintUUID})
	})
	if err != nil {
		t.Fatalf("runSprintGet: %v", err)
	}
	if !strings.Contains(out, "Sprint 12") {
		t.Fatalf("output missing sprint name: %s", out)
	}
}

func TestRunSprintGetByNameResolvesViaWorkspaceList(t *testing.T) {
	_ = sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/sprints":
			_ = json.NewEncoder(w).Encode(map[string]any{"sprints": []any{testSprintObject()}})
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newSprintTestCmd(nil)
	out, err := captureStdout(t, func() error {
		return runSprintGet(cmd, []string{"sprint 12"})
	})
	if err != nil {
		t.Fatalf("runSprintGet by name: %v", err)
	}
	if !strings.Contains(out, testSprintUUID) {
		t.Fatalf("output missing sprint id: %s", out)
	}
}

func TestRunSprintGetFallsBackToLegacyProject(t *testing.T) {
	paths := sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/sprints/"+testProjectUUID:
			http.Error(w, `{"error":"sprint not found"}`, http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/"+testProjectUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": testProjectUUID, "title": "Sprint A (legacy)", "status": "in_progress",
			})
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newSprintTestCmd(nil)
	out, err := captureStdout(t, func() error {
		return runSprintGet(cmd, []string{testProjectUUID})
	})
	if err != nil {
		t.Fatalf("runSprintGet legacy fallback: %v", err)
	}
	if !strings.Contains(out, "Sprint A (legacy)") {
		t.Fatalf("output missing legacy project: %s", out)
	}
	joined := strings.Join(*paths, " | ")
	if !strings.Contains(joined, "GET /api/cerebro/sprints/"+testProjectUUID) {
		t.Fatalf("sprint endpoint was not tried first: %v", *paths)
	}
}

func TestRunSprintIssuesListsIssuesWithStatus(t *testing.T) {
	_ = sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/sprints/"+testSprintUUID:
			_ = json.NewEncoder(w).Encode(testSprintObject())
		case r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/sprints/"+testSprintUUID+"/issues":
			_ = json.NewEncoder(w).Encode(map[string]any{"issues": []any{map[string]any{
				"issue_id":   testSprintIssueUUID,
				"sprint_id":  testSprintUUID,
				"identifier": "FIR-2500",
				"number":     2500,
				"title":      "Sprint context in CLI",
				"status":     "in_progress",
				"priority":   "high",
			}}})
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newSprintTestCmd(nil)
	out, err := captureStdout(t, func() error {
		return runSprintIssues(cmd, []string{testSprintUUID})
	})
	if err != nil {
		t.Fatalf("runSprintIssues: %v", err)
	}
	for _, want := range []string{"FIR-2500", "in_progress", "Sprint context in CLI"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q: %s", want, out)
		}
	}
}

func TestRunSprintAssignPutsIssueSprint(t *testing.T) {
	var gotBody map[string]any
	_ = sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testSprintIssueUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": testSprintIssueUUID, "identifier": "FIR-1", "title": "t",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/sprints/"+testSprintUUID:
			_ = json.NewEncoder(w).Encode(testSprintObject())
		case r.Method == http.MethodPut && r.URL.Path == "/api/cerebro/issues/"+testSprintIssueUUID+"/sprint":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newSprintTestCmd(nil)
	out, err := captureStdout(t, func() error {
		return runSprintAssign(cmd, []string{testSprintIssueUUID, testSprintUUID})
	})
	if err != nil {
		t.Fatalf("runSprintAssign: %v", err)
	}
	if gotBody["sprint_id"] != testSprintUUID {
		t.Fatalf("assign body = %v, want sprint_id %s", gotBody, testSprintUUID)
	}
	if !strings.Contains(out, `"assigned": true`) {
		t.Fatalf("output missing assigned confirmation: %s", out)
	}
}

func TestRunSprintAssignNoneRemovesFromSprint(t *testing.T) {
	var gotBody map[string]any
	_ = sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testSprintIssueUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": testSprintIssueUUID, "identifier": "FIR-1", "title": "t",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/cerebro/issues/"+testSprintIssueUUID+"/sprint":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newSprintTestCmd(nil)
	out, err := captureStdout(t, func() error {
		return runSprintAssign(cmd, []string{testSprintIssueUUID, "none"})
	})
	if err != nil {
		t.Fatalf("runSprintAssign none: %v", err)
	}
	if gotBody["sprint_id"] != "" {
		t.Fatalf("assign body = %v, want empty sprint_id", gotBody)
	}
	if !strings.Contains(out, `"removed": true`) {
		t.Fatalf("output missing removed confirmation: %s", out)
	}
}

func TestAssignCreatedIssueToUISprintResolvesByName(t *testing.T) {
	var gotBody map[string]any
	_ = sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/sprints":
			_ = json.NewEncoder(w).Encode(map[string]any{"sprints": []any{testSprintObject()}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/cerebro/issues/"+testSprintIssueUUID+"/sprint":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newSprintTestCmd(map[string]string{"sprint": "Sprint 12"})
	client, err := newAPIClient(cmd)
	if err != nil {
		t.Fatalf("newAPIClient: %v", err)
	}
	ctx, cancel := newSprintContext()
	defer cancel()
	if err := assignCreatedIssueToUISprint(ctx, cmd, client, testSprintIssueUUID); err != nil {
		t.Fatalf("assignCreatedIssueToUISprint: %v", err)
	}
	if gotBody["sprint_id"] != testSprintUUID {
		t.Fatalf("assign body = %v, want sprint_id %s", gotBody, testSprintUUID)
	}
}

func TestAssignCreatedIssueToUISprintNoFlagIsNoop(t *testing.T) {
	paths := sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	cmd := newSprintTestCmd(nil)
	client, err := newAPIClient(cmd)
	if err != nil {
		t.Fatalf("newAPIClient: %v", err)
	}
	ctx, cancel := newSprintContext()
	defer cancel()
	if err := assignCreatedIssueToUISprint(ctx, cmd, client, testSprintIssueUUID); err != nil {
		t.Fatalf("expected noop, got: %v", err)
	}
	if len(*paths) != 0 {
		t.Fatalf("expected no requests, got: %v", *paths)
	}
}

func TestResolveSprintAmbiguousNameFails(t *testing.T) {
	second := testSprintObject()
	second["id"] = "50003764-b62b-4052-a6a4-40f1e6b13021"
	second["project_title"] = "Other project"
	_ = sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/sprints" {
			_ = json.NewEncoder(w).Encode(map[string]any{"sprints": []any{testSprintObject(), second}})
			return
		}
		http.NotFound(w, r)
	})

	cmd := newSprintTestCmd(nil)
	client, err := newAPIClient(cmd)
	if err != nil {
		t.Fatalf("newAPIClient: %v", err)
	}
	ctx, cancel := newSprintContext()
	defer cancel()
	_, _, resolveErr := resolveSprint(ctx, client, "Sprint 12")
	if resolveErr == nil || !strings.Contains(resolveErr.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got: %v", resolveErr)
	}
}

func TestRunSprintCreateUsesSprintFeatureWhenSettingsExist(t *testing.T) {
	var gotBody map[string]any
	_ = sprintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/cerebro/projects/"+testProjectUUID+"/sprint-settings":
			_ = json.NewEncoder(w).Encode(map[string]any{"project_id": testProjectUUID, "enabled": true})
		case r.Method == http.MethodPost && r.URL.Path == "/api/cerebro/projects/"+testProjectUUID+"/sprints":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(testSprintObject())
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newSprintTestCmd(map[string]string{
		"name":  "Sprint 13",
		"start": "2026-07-20",
		"end":   "2026-08-02",
	})
	out, err := captureStdout(t, func() error {
		return runSprintCreate(cmd, []string{testProjectUUID})
	})
	if err != nil {
		t.Fatalf("runSprintCreate: %v", err)
	}
	if gotBody["name"] != "Sprint 13" || gotBody["start_date"] != "2026-07-20" || gotBody["end_date"] != "2026-08-02" {
		t.Fatalf("create body = %v", gotBody)
	}
	if !strings.Contains(out, testSprintUUID) {
		t.Fatalf("output missing created sprint: %s", out)
	}
}
