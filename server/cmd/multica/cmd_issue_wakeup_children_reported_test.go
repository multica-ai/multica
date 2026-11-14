package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestChildrenReportedCLICreateAndUpdate(t *testing.T) {
	t.Chdir(t.TempDir())
	const issue = "a57c0511-1ebc-471d-a314-438ca16cc75d"
	var body map[string]any
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "wake", "condition": body["condition"]})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "fixture")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	for _, action := range []string{"create", "update"} {
		for _, stage := range []bool{false, true} {
			args := []string{action, issue}
			wantMethod := "POST"
			if action == "update" {
				args = append(args, "wake")
				wantMethod = "PUT"
			}
			args = append(args, "--until-children-reported", "--instruction", "Inspect")
			want := map[string]any{"type": "children_reported"}
			if stage {
				args = append(args, "--stage", "2")
				want["stage"] = float64(2)
			}
			cmd := newIssueWakeupCommand()
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if method != wantMethod || !reflect.DeepEqual(body["condition"], want) {
				t.Fatalf("%s %v", method, body)
			}
		}
	}
}

func TestChildrenReportedCLIRejectsConflictingUntilAndStage(t *testing.T) {
	for _, flags := range [][]string{
		{"--until-children-reported", "--until-children-done"},
		{"--until-children-reported", "--until-status", "in_review"},
		{"--until-children-reported", "--until-pr", "merged"},
		{"--until-children-reported", "--stage", "0"},
		{"--until-children-reported", "--stage", "-1"},
		{"--until-children-reported", "--stage", "1001"},
		{"--until-children-reported", "--event", "comment.created"},
		{"--until-children-reported", "--filter-agent-id", "agent"},
		{"--until-children-reported", "--task-id", "task"},
		{"--until-children-reported", "--filter-actor-type", "member", "--filter-actor-id", "user"},
		{"--until-children-reported", "--kind", "at", "--after", "1h"},
		{"--until-children-reported", "--every", "1h"},
		{"--stage", "2"},
	} {
		cmd := newIssueWakeupCommand()
		create, _, err := cmd.Find([]string{"create"})
		if err != nil {
			t.Fatal(err)
		}
		if err := create.ParseFlags(flags); err != nil {
			t.Fatal(err)
		}
		if _, err := wakeupConditionFromFlags(t.Context(), create, nil); err == nil {
			t.Fatalf("accepted %v", flags)
		}
	}
}
