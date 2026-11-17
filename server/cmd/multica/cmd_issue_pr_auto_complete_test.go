package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const prAutoCompleteTestIssueID = "11111111-1111-4111-8111-111111111111"

func TestIssuePRAutoCompleteFlagRegistration(t *testing.T) {
	for _, cmd := range []*cobra.Command{issueCreateCmd, issueUpdateCmd} {
		if cmd.Flags().Lookup("pr-auto-complete") == nil {
			t.Fatalf("%s has no --pr-auto-complete flag", cmd.Name())
		}
	}
}

func TestIssuePRAutoCompleteRejectsInvalidValuesBeforeRequest(t *testing.T) {
	for _, value := range []string{"", "true", "false", "ON", "disabled"} {
		t.Run(value, func(t *testing.T) {
			for _, create := range []bool{true, false} {
				cmd := newIssueUpdateTestCmd()
				run := func() error { return runIssueUpdate(cmd, []string{prAutoCompleteTestIssueID}) }
				if create {
					cmd = newIssueCreateTestCmd()
					_ = cmd.Flags().Set("title", "Multi PR work")
					run = func() error { return runIssueCreate(cmd, nil) }
				}
				cmd.Flags().String("pr-auto-complete", "", "")
				_ = cmd.Flags().Set("pr-auto-complete", value)
				if err := run(); err == nil || !strings.Contains(err.Error(), "must be on or off") {
					t.Fatalf("create=%v value=%q: %v", create, value, err)
				}
			}
		})
	}
}

func TestRunIssueUpdatePRAutoComplete(t *testing.T) {
	for _, tc := range []struct {
		name, value, title string
		putStatus          int
		wantError          string
	}{
		{name: "disable only", value: "off"},
		{name: "enable only", value: "on"},
		{name: "ordinary fields first", value: "off", title: "Updated"},
		{name: "partial failure", value: "off", title: "Updated", putStatus: http.StatusForbidden, wantError: "fields were updated"},
		{name: "unsupported endpoint", value: "on", putStatus: http.StatusNotFound, wantError: "set issue PR auto-complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var calls []string
			path := "/api/issues/" + prAutoCompleteTestIssueID
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case path:
					if r.Method == http.MethodPut {
						var body map[string]any
						_ = json.NewDecoder(r.Body).Decode(&body)
						if body["title"] != tc.title || body["suppress_run"] != true || len(body) != 2 {
							t.Errorf("ordinary update body = %v", body)
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": prAutoCompleteTestIssueID, "identifier": "MUL-8688", "title": "Multi PR work"})
				case path + "/pr-auto-complete":
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if r.Method != http.MethodPut || len(body) != 1 || body["disabled"] != (tc.value == "off") {
						t.Errorf("automation request = %s %v", r.Method, body)
					}
					if tc.putStatus != 0 {
						http.Error(w, `{"error":"rejected"}`, tc.putStatus)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"auto_complete": map[string]any{"issue_disabled": tc.value == "off"}})
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			cmd := newIssueUpdateTestCmd()
			cmd.Flags().String("pr-auto-complete", "", "")
			_ = cmd.Flags().Set("pr-auto-complete", tc.value)
			_ = cmd.Flags().Set("no-start", "true")
			if tc.title != "" {
				_ = cmd.Flags().Set("title", tc.title)
			}
			out, err := captureStdout(t, func() error { return runIssueUpdate(cmd, []string{prAutoCompleteTestIssueID}) })
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) || out != "" {
					t.Fatalf("out=%q err=%v, want %q", out, err, tc.wantError)
				}
			} else {
				var result map[string]any
				if err != nil || json.Unmarshal([]byte(out), &result) != nil || result["id"] != prAutoCompleteTestIssueID || result["pr_auto_complete_disabled"] != (tc.value == "off") {
					t.Fatalf("out=%q err=%v", out, err)
				}
			}
			firstMethod := http.MethodGet
			if tc.title != "" {
				firstMethod = http.MethodPut
			}
			if want := []string{firstMethod + " " + path, "PUT " + path + "/pr-auto-complete"}; !slices.Equal(calls, want) {
				t.Fatalf("calls=%v, want %v", calls, want)
			}
		})
	}
}

func TestRunIssueCreatePRAutoComplete(t *testing.T) {
	for _, tc := range []struct {
		name, value, echo, putResponse string
		putStatus                      int
		wantError                      bool
	}{
		{name: "atomic off", value: "off", echo: `,"pr_auto_complete_disabled":true`},
		{name: "atomic on", value: "on", echo: `,"pr_auto_complete_disabled":false`},
		{name: "omitted stays default"},
		{name: "old server fallback off", value: "off", putResponse: `{"auto_complete":{"issue_disabled":true}}`},
		{name: "old server fallback on", value: "on", putResponse: `{"auto_complete":{"issue_disabled":false}}`},
		{name: "fallback fails", value: "off", putStatus: http.StatusNotFound, wantError: true},
		{name: "fallback missing confirmation", value: "off", putResponse: `{}`, wantError: true},
		{name: "fallback wrong confirmation", value: "off", putResponse: `{"auto_complete":{"issue_disabled":false}}`, wantError: true},
		{name: "wrong echo", value: "off", echo: `,"pr_auto_complete_disabled":false`, wantError: true},
		{name: "malformed echo", value: "off", echo: `,"pr_auto_complete_disabled":"off"`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var calls []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case "/api/issues":
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					value, present := body["pr_auto_complete_disabled"]
					if r.Method != http.MethodPost || present != (tc.value != "") || (present && value != (tc.value == "off")) {
						t.Errorf("create request = %s %v", r.Method, body)
					}
					_, _ = w.Write([]byte(`{"id":"` + prAutoCompleteTestIssueID + `","identifier":"MUL-8688","title":"Multi PR work"` + tc.echo + `}`))
				case "/api/issues/" + prAutoCompleteTestIssueID + "/pr-auto-complete":
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if r.Method != http.MethodPut || body["disabled"] != (tc.value == "off") {
						t.Errorf("fallback request = %s %v", r.Method, body)
					}
					if tc.putStatus != 0 {
						http.Error(w, `{"error":"unsupported"}`, tc.putStatus)
						return
					}
					_, _ = w.Write([]byte(tc.putResponse))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			cmd := newIssueCreateTestCmd()
			cmd.Flags().String("pr-auto-complete", "", "")
			_ = cmd.Flags().Set("title", "Multi PR work")
			if tc.value != "" {
				_ = cmd.Flags().Set("pr-auto-complete", tc.value)
			}
			out, err := captureStdout(t, func() error { return runIssueCreate(cmd, nil) })
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), prAutoCompleteTestIssueID) || !strings.Contains(err.Error(), "do not repeat issue create") || out != "" {
					t.Fatalf("out=%q err=%v; want recoverable partial-create error", out, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			want := []string{"POST /api/issues"}
			if tc.value != "" && tc.echo == "" {
				want = append(want, "PUT /api/issues/"+prAutoCompleteTestIssueID+"/pr-auto-complete")
			}
			if !slices.Equal(calls, want) {
				t.Fatalf("calls=%v, want %v", calls, want)
			}
		})
	}
}

func TestRunIssuePullRequestsPRAutoCompleteOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	autoComplete := map[string]any{"issue_disabled": true, "workspace_enabled": false, "state": "workspace_disabled", "target_status": "none"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"pull_requests": []any{}, "auto_complete": autoComplete})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	for _, format := range []string{"table", "json"} {
		cmd := newIssuePullRequestsTestCmd()
		_ = cmd.Flags().Set("output", format)
		out, err := captureStdout(t, func() error { return runIssuePullRequests(cmd, []string{prAutoCompleteTestIssueID}) })
		if err != nil {
			t.Fatal(err)
		}
		if format == "table" {
			if !strings.Contains(out, "PR auto-complete: off | Workspace: off | Target: none | State: workspace_disabled") {
				t.Fatalf("missing automation state with no PRs: %q", out)
			}
		} else {
			var result map[string]any
			if json.Unmarshal([]byte(out), &result) != nil || result["auto_complete"].(map[string]any)["issue_disabled"] != true {
				t.Fatalf("JSON output lost automation: %q", out)
			}
		}
	}
}
