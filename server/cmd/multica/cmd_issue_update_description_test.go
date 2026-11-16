package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRunIssueUpdateDescriptionInputs(t *testing.T) {
	const issueID = "11111111-1111-4111-8111-111111111111"
	const attachmentID = "33333333-3333-4333-8333-333333333333"
	const existing = "Existing body"
	const reference = "!file[fixture.txt](https://example.invalid/api/attachments/" + attachmentID + "/download)"
	for _, tt := range []struct {
		name        string
		args        []string
		stdin       string
		want        string
		methods     []string
		wantError   string
		omitDesc    bool
		attachment  bool
		noStart     bool
		failRequest bool
	}{
		{name: "omitted", args: []string{"--title", "Updated"}, want: existing, methods: []string{"PUT"}, omitDesc: true},
		{name: "stdin false with title", args: []string{"--description-stdin=false", "--title", "Updated"}, want: existing, methods: []string{"PUT"}, omitDesc: true},
		{name: "stdin false alone", args: []string{"--description-stdin=false"}, want: existing, wantError: "no fields to update"},
		{name: "explicit empty clears", args: []string{"--description", ""}, want: "", methods: []string{"PUT"}},
		{name: "stdin false with empty clears", args: []string{"--description-stdin=false", "--description", ""}, want: "", methods: []string{"PUT"}},
		{name: "stdin false with inline", args: []string{"--description-stdin=false", "--description", "Inline"}, want: "Inline", methods: []string{"PUT"}},
		{name: "file", args: []string{"--description-file", "fixture.txt"}, want: "File body 中文", methods: []string{"PUT"}},
		{name: "stdin false with file", args: []string{"--description-stdin=false", "--description-file", "fixture.txt"}, want: "File body 中文", methods: []string{"PUT"}},
		{name: "stdin", args: []string{"--description-stdin"}, stdin: "Stdin body\n", want: "Stdin body", methods: []string{"PUT"}},
		{name: "empty stdin", args: []string{"--description-stdin"}, want: existing, wantError: "stdin content for --description-stdin is empty"},
		{name: "stdin inline conflict", args: []string{"--description-stdin", "--description", "Inline"}, stdin: "Stdin", want: existing, wantError: "mutually exclusive"},
		{name: "stdin file conflict", args: []string{"--description-stdin", "--description-file", "fixture.txt"}, stdin: "Stdin", want: existing, wantError: "mutually exclusive"},
		{name: "attachment preserves", args: []string{"--attachment", "fixture.txt"}, want: existing + "\n\n" + reference, methods: []string{"GET", "POST", "PUT"}, attachment: true},
		{name: "stdin false with attachment", args: []string{"--description-stdin=false", "--attachment", "fixture.txt"}, want: existing + "\n\n" + reference, methods: []string{"GET", "POST", "PUT"}, attachment: true},
		{name: "clear with attachment", args: []string{"--description", "", "--attachment", "fixture.txt"}, want: reference, methods: []string{"POST", "PUT"}, attachment: true},
		{name: "stdin false no start", args: []string{"--description-stdin=false", "--title", "Updated", "--no-start"}, want: existing, methods: []string{"PUT"}, omitDesc: true, noStart: true},
		{name: "request failure", args: []string{"--description-stdin=false", "--title", "Updated"}, want: existing, methods: []string{"PUT"}, wantError: "update issue", omitDesc: true, failRequest: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("fixture.txt", []byte("File body 中文\n"), 0600); err != nil {
				t.Fatal(err)
			}
			stored := map[string]any{"id": issueID, "description": existing, "title": "Original"}
			var methods []string
			var update map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				methods = append(methods, r.Method)
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/api/upload-file":
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Errorf("parse upload: %v", err)
					}
					if r.FormValue("issue_id") != "" {
						t.Error("attachment should be bound by the update")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": attachmentID, "filename": "fixture.txt", "content_type": "text/plain", "markdown_url": "https://example.invalid/api/attachments/" + attachmentID + "/download"})
				case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+issueID:
					_ = json.NewEncoder(w).Encode(stored)
				case r.Method == http.MethodPut && r.URL.Path == "/api/issues/"+issueID:
					if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
						t.Errorf("decode update: %v", err)
					}
					if tt.failRequest {
						http.Error(w, "synthetic failure", http.StatusInternalServerError)
						return
					}
					for key, value := range update {
						stored[key] = value
					}
					_ = json.NewEncoder(w).Encode(stored)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			t.Setenv("MULTICA_TOKEN", "mat_synthetic_description_test")
			cmd := newIssueUpdateTestCmd()
			cmd.RunE = runIssueUpdate
			cmd.SetArgs(append([]string{issueID}, tt.args...))
			pipeStdin(t, tt.stdin, func() {
				err := cmd.Execute()
				if tt.wantError == "" && err != nil {
					t.Errorf("execute: %v", err)
				}
				if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
					t.Errorf("error = %v, want %q", err, tt.wantError)
				}
			})
			if !reflect.DeepEqual(methods, tt.methods) {
				t.Errorf("request methods = %v, want %v", methods, tt.methods)
			}
			if got := stored["description"]; got != tt.want {
				t.Errorf("stored description = %q, want %q", got, tt.want)
			}
			if tt.omitDesc {
				if _, present := update["description"]; present {
					t.Errorf("description must be omitted: %#v", update)
				}
			}
			if tt.omitDesc && !tt.failRequest && stored["title"] != "Updated" {
				t.Errorf("stored title = %#v", stored["title"])
			}
			if tt.attachment && !reflect.DeepEqual(update["attachment_ids"], []any{attachmentID}) {
				t.Errorf("attachment_ids = %#v", update["attachment_ids"])
			}
			if tt.noStart && update["suppress_run"] != true {
				t.Errorf("suppress_run = %#v", update["suppress_run"])
			}
		})
	}
}
