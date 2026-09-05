package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func projectPlanTestCommand(flagSet string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")
	switch flagSet {
	case "create":
		cmd.Flags().String("kind", "prd", "")
	case "phase":
		cmd.Flags().String("title", "", "")
		cmd.Flags().String("description", "", "")
		cmd.Flags().Int32("position", 0, "")
	case "part":
		cmd.Flags().String("title", "", "")
		cmd.Flags().String("description", "", "")
		cmd.Flags().String("acceptance-criteria", "", "")
		cmd.Flags().Int32("position", 0, "")
	}
	return cmd
}

func TestProjectPlanAuthoringCommandsRegistered(t *testing.T) {
	for _, name := range []string{"create-from-issue", "add-phase", "add-part", "link-issue"} {
		if _, _, err := projectPlanCmd.Find([]string{name}); err != nil {
			t.Errorf("project plan command %q is not registered: %v", name, err)
		}
	}
}

func TestProjectPlanAgentAuthoringSequence(t *testing.T) {
	const (
		projectID = "11111111-1111-1111-1111-111111111111"
		sourceID  = "22222222-2222-2222-2222-222222222222"
		planID    = "33333333-3333-3333-3333-333333333333"
		phaseID   = "44444444-4444-4444-4444-444444444444"
		partID    = "55555555-5555-5555-5555-555555555555"
		issueID   = "66666666-6666-6666-6666-666666666666"
		agentID   = "77777777-7777-7777-7777-777777777777"
		taskID    = "88888888-8888-8888-8888-888888888888"
	)

	type requestRecord struct {
		method string
		path   string
		body   map[string]any
	}
	var requests []requestRecord
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Agent-ID"); got != agentID {
			t.Errorf("X-Agent-ID = %q, want %q", got, agentID)
		}
		if got := r.Header.Get("X-Task-ID"); got != taskID {
			t.Errorf("X-Task-ID = %q, want %q", got, taskID)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		requests = append(requests, requestRecord{method: r.Method, path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		switch len(requests) {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": planID, "title": "Source PRD"})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": phaseID, "title": "Build"})
		case 3:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": partID, "title": "API"})
		case 4:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "99999999-9999-9999-9999-999999999999", "issue_id": issueID})
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	t.Setenv("MULTICA_AGENT_ID", agentID)
	t.Setenv("MULTICA_TASK_ID", taskID)

	createCmd := projectPlanTestCommand("create")
	if _, err := captureStdout(t, func() error {
		return runProjectPlanCreateFromIssue(createCmd, []string{projectID, sourceID})
	}); err != nil {
		t.Fatalf("create from issue: %v", err)
	}

	phaseCmd := projectPlanTestCommand("phase")
	_ = phaseCmd.Flags().Set("title", "Build")
	_ = phaseCmd.Flags().Set("description", "Implementation phase")
	_ = phaseCmd.Flags().Set("position", "0")
	if _, err := captureStdout(t, func() error {
		return runProjectPlanAddPhase(phaseCmd, []string{projectID, planID})
	}); err != nil {
		t.Fatalf("add phase: %v", err)
	}

	partCmd := projectPlanTestCommand("part")
	_ = partCmd.Flags().Set("title", "API")
	_ = partCmd.Flags().Set("description", "Expose authoring")
	_ = partCmd.Flags().Set("acceptance-criteria", "Agent can author")
	_ = partCmd.Flags().Set("position", "0")
	if _, err := captureStdout(t, func() error {
		return runProjectPlanAddPart(partCmd, []string{projectID, planID, phaseID})
	}); err != nil {
		t.Fatalf("add part: %v", err)
	}

	linkCmd := projectPlanTestCommand("")
	if _, err := captureStdout(t, func() error {
		return runProjectPlanLinkIssue(linkCmd, []string{projectID, planID, partID, issueID})
	}); err != nil {
		t.Fatalf("link issue: %v", err)
	}

	want := []requestRecord{
		{method: http.MethodPost, path: "/api/projects/" + projectID + "/plans/from-issue", body: map[string]any{"kind": "prd", "source_issue_id": sourceID}},
		{method: http.MethodPost, path: "/api/projects/" + projectID + "/plans/" + planID + "/phases", body: map[string]any{"title": "Build", "description": "Implementation phase", "position": float64(0)}},
		{method: http.MethodPost, path: "/api/projects/" + projectID + "/plans/" + planID + "/phases/" + phaseID + "/parts", body: map[string]any{"title": "API", "description": "Expose authoring", "acceptance_criteria": "Agent can author", "position": float64(0)}},
		{method: http.MethodPost, path: "/api/projects/" + projectID + "/plans/" + planID + "/parts/" + partID + "/issues/" + issueID, body: map[string]any{}},
	}
	if len(requests) != len(want) {
		t.Fatalf("request count = %d, want %d: %+v", len(requests), len(want), requests)
	}
	for i := range want {
		if requests[i].method != want[i].method || requests[i].path != want[i].path {
			t.Errorf("request %d = %s %s, want %s %s", i, requests[i].method, requests[i].path, want[i].method, want[i].path)
		}
		gotBody, _ := json.Marshal(requests[i].body)
		wantBody, _ := json.Marshal(want[i].body)
		if string(gotBody) != string(wantBody) {
			t.Errorf("request %d body = %s, want %s", i, gotBody, wantBody)
		}
	}
}
