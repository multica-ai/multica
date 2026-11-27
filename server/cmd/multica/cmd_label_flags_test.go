package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const (
	labelFlagBugID      = "11111111-aaaa-4aaa-8aaa-111111111111"
	labelFlagFeedbackID = "22222222-bbbb-4bbb-8bbb-222222222222"
)

// newLabelFlagServer serves the issue label list and records the body of the
// write sent to writePath. echo shapes the write response from that body.
func newLabelFlagServer(t *testing.T, writePath string, echo func(body map[string]any) map[string]any) (*httptest.Server, *map[string]any, *int) {
	t.Helper()
	var body map[string]any
	labelLists := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/labels":
			if got := r.URL.Query().Get("resource_type"); got != "issue" {
				t.Errorf("label list resource_type = %q, want issue", got)
			}
			labelLists++
			json.NewEncoder(w).Encode(map[string]any{"labels": []map[string]any{
				{"id": labelFlagBugID, "name": "Bug", "color": "#ef4444"},
				{"id": labelFlagFeedbackID, "name": "User feedback", "color": "#22c55e"},
			}})
		case writePath:
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			json.NewEncoder(w).Encode(echo(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	return srv, &body, &labelLists
}

func echoIssueLabels(body map[string]any) map[string]any {
	labels := []map[string]any{}
	ids, _ := body["label_ids"].([]any)
	for _, id := range ids {
		labels = append(labels, map[string]any{"id": id})
	}
	return map[string]any{"id": "issue-1", "identifier": "MUL-1", "title": "t", "labels": labels}
}

func TestRunIssueCreateSendsLabelIDs(t *testing.T) {
	clearDaemonTaskEnv(t)
	_, body, labelLists := newLabelFlagServer(t, "/api/issues", echoIssueLabels)

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "Crash on launch")
	_ = cmd.Flags().Set("label", "user FEEDBACK")
	_ = cmd.Flags().Set("label", "11111111")
	_ = cmd.Flags().Set("label", labelFlagFeedbackID)
	if err := runIssueCreate(cmd, nil); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}
	want := []any{labelFlagFeedbackID, labelFlagBugID}
	if got := (*body)["label_ids"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("label_ids = %#v, want %#v", got, want)
	}
	if *labelLists != 1 {
		t.Fatalf("label list fetched %d times, want 1", *labelLists)
	}
}

func TestRunIssueCreateFullUUIDLabelSkipsLabelList(t *testing.T) {
	clearDaemonTaskEnv(t)
	_, body, labelLists := newLabelFlagServer(t, "/api/issues", echoIssueLabels)

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "Crash on launch")
	_ = cmd.Flags().Set("label", labelFlagBugID)
	if err := runIssueCreate(cmd, nil); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}
	if got := (*body)["label_ids"]; !reflect.DeepEqual(got, []any{labelFlagBugID}) {
		t.Fatalf("label_ids = %#v", got)
	}
	if *labelLists != 0 {
		t.Fatalf("label list fetched %d times, want 0", *labelLists)
	}
}

func TestRunIssueCreateUnknownLabelFailsBeforePost(t *testing.T) {
	clearDaemonTaskEnv(t)
	_, body, _ := newLabelFlagServer(t, "/api/issues", echoIssueLabels)

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "Crash on launch")
	_ = cmd.Flags().Set("label", "Nope")
	err := runIssueCreate(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), `no issue label named "Nope"`) {
		t.Fatalf("runIssueCreate error = %v, want unknown label", err)
	}
	if *body != nil {
		t.Fatalf("issue was posted despite the unknown label: %#v", *body)
	}
}

func TestRunIssueCreateReportsLabelsTheServerDropped(t *testing.T) {
	clearDaemonTaskEnv(t)
	newLabelFlagServer(t, "/api/issues", func(map[string]any) map[string]any {
		// A server that predates label_ids returns no labels snapshot.
		return map[string]any{"id": "issue-1", "identifier": "MUL-1", "title": "t"}
	})

	cmd := newIssueCreateTestCmd()
	_ = cmd.Flags().Set("title", "Crash on launch")
	_ = cmd.Flags().Set("label", "Bug")
	err := runIssueCreate(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "MUL-1 was created, but the server did not confirm its labels") {
		t.Fatalf("runIssueCreate error = %v, want unconfirmed labels", err)
	}
}

func TestMatchIssueLabelPrefersExactNameAndRejectsAmbiguity(t *testing.T) {
	candidates := []idCandidate{
		{ID: "abcdef01-0000-4000-8000-000000000001", Display: "abcdef01"},
		{ID: "abcdef01-0000-4000-8000-000000000002", Display: "Two"},
		{ID: "abcdef01-0000-4000-8000-000000000003", Display: "two"},
	}
	if got, err := matchIssueLabel(candidates, "ABCDEF01"); err != nil || got != candidates[0].ID {
		t.Fatalf("name match = %q, %v; want %s", got, err, candidates[0].ID)
	}
	if _, err := matchIssueLabel(candidates, "two"); err == nil || !strings.Contains(err.Error(), "matches 2 labels") {
		t.Fatalf("duplicate name error = %v", err)
	}
	if _, err := matchIssueLabel(candidates, "abcdef0"); err == nil || !strings.Contains(err.Error(), "matches 3 labels") {
		t.Fatalf("ambiguous prefix error = %v", err)
	}
}

func echoAutopilotLabels(body map[string]any) map[string]any {
	ids, ok := body["label_ids"]
	if !ok {
		ids = []any{}
	}
	return map[string]any{"id": "autopilot-1", "title": "Feedback intake", "label_ids": ids}
}

func TestRunAutopilotCreateSendsLabelIDs(t *testing.T) {
	const agentID = "33333333-3333-3333-3333-333333333333"
	_, body, _ := newLabelFlagServer(t, "/api/autopilots", echoAutopilotLabels)

	cmd := newAutopilotCreateTestCmd()
	_ = cmd.Flags().Set("title", "Feedback intake")
	_ = cmd.Flags().Set("agent", agentID)
	_ = cmd.Flags().Set("mode", "create_issue")
	_ = cmd.Flags().Set("label", "User feedback")
	if err := runAutopilotCreate(cmd, nil); err != nil {
		t.Fatalf("runAutopilotCreate: %v", err)
	}
	if got := (*body)["label_ids"]; !reflect.DeepEqual(got, []any{labelFlagFeedbackID}) {
		t.Fatalf("label_ids = %#v", got)
	}
}

func TestRunAutopilotCreateReportsServerWithoutLabels(t *testing.T) {
	const agentID = "33333333-3333-3333-3333-333333333333"
	newLabelFlagServer(t, "/api/autopilots", func(map[string]any) map[string]any {
		return map[string]any{"id": "autopilot-1", "title": "Feedback intake"}
	})

	cmd := newAutopilotCreateTestCmd()
	_ = cmd.Flags().Set("title", "Feedback intake")
	_ = cmd.Flags().Set("agent", agentID)
	_ = cmd.Flags().Set("mode", "create_issue")
	_ = cmd.Flags().Set("label", "Bug")
	err := runAutopilotCreate(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "does not support autopilot labels") {
		t.Fatalf("runAutopilotCreate error = %v, want unsupported labels", err)
	}
}

func TestRunAutopilotUpdateLabelFlags(t *testing.T) {
	const autopilotID = "44444444-4444-4444-4444-444444444444"
	for _, tc := range []struct {
		name  string
		flags map[string]string
		want  []any
	}{
		{name: "replace", flags: map[string]string{"label": "Bug"}, want: []any{labelFlagBugID}},
		{name: "clear", flags: map[string]string{"clear-labels": "true"}, want: []any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, body, _ := newLabelFlagServer(t, "/api/autopilots/"+autopilotID, echoAutopilotLabels)
			cmd := newAutopilotUpdateTestCmd()
			for k, v := range tc.flags {
				_ = cmd.Flags().Set(k, v)
			}
			if err := runAutopilotUpdate(cmd, []string{autopilotID}); err != nil {
				t.Fatalf("runAutopilotUpdate: %v", err)
			}
			if got := (*body)["label_ids"]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("label_ids = %#v, want %#v", got, tc.want)
			}
		})
	}

	newLabelFlagServer(t, "/api/autopilots/"+autopilotID, echoAutopilotLabels)
	cmd := newAutopilotUpdateTestCmd()
	_ = cmd.Flags().Set("label", "Bug")
	_ = cmd.Flags().Set("clear-labels", "true")
	if err := runAutopilotUpdate(cmd, []string{autopilotID}); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("runAutopilotUpdate error = %v, want mutually exclusive", err)
	}
}
