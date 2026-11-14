package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Exercise the registered command tree in fresh processes using the existing
// argv fixture. This covers parsing, RunE and JSON serialization together.
func TestTextFlagRequests(t *testing.T) {
	const issueID = "11111111-1111-4111-8111-111111111111"
	const literal = `tools\texconv.exe C:\repo\new \\server\share \times \theta ^\n\t\r$`
	const mixed = "中文 Русский\r\n" + literal + "\tactual tab\rbare CR\n\n"
	commands := []struct {
		name                  string
		args                  []string
		method, path          string
		flag, field           string
		stdin, file           bool
		required, emptyAbsent bool
		omittedPresent        bool
	}{
		{
			name: "issue create", args: []string{"issue", "create", "--title", "text fixture"},
			method: "POST", path: "/api/issues", flag: "description", field: "description",
			stdin: true, file: true, emptyAbsent: true,
		},
		{
			name: "issue update", args: []string{"issue", "update", issueID, "--title", "text fixture"},
			method: "PUT", path: "/api/issues/" + issueID, flag: "description", field: "description",
			stdin: true, file: true,
		},
		{
			name: "comment add", args: []string{"issue", "comment", "add", issueID},
			method: "POST", path: "/api/issues/" + issueID + "/comments", flag: "content", field: "content",
			stdin: true, file: true, required: true,
		},
		{
			name: "comment update", args: []string{"issue", "comment", "update", issueID, "--expected-revision", "1"},
			method: "PUT", path: "/api/comments/" + issueID, flag: "content", field: "content",
			stdin: true, file: true, required: true,
		},
		{
			name: "wakeup create", args: []string{"issue", "wakeup", "create", issueID, "--event", "task.completed"},
			method: "POST", path: "/api/issues/" + issueID + "/wakeups", flag: "instruction", field: "instruction",
			file: true, omittedPresent: true,
		},
		{
			name: "wakeup update", args: []string{"issue", "wakeup", "update", issueID, "wake", "--event", "task.completed"},
			method: "PUT", path: "/api/issues/" + issueID + "/wakeups/wake", flag: "instruction", field: "instruction",
			file: true, omittedPresent: true,
		},
		{
			name: "user profile", args: []string{"user", "profile", "update"},
			method: "PATCH", path: "/api/me", flag: "description", field: "profile_description",
			stdin: true, file: true,
		},
		{
			name: "workspace create description", args: []string{"workspace", "create", "--name", "text fixture", "--slug", "text-fixture"},
			method: "POST", path: "/api/workspaces", flag: "description", field: "description",
			stdin: true,
		},
		{
			name: "workspace create context", args: []string{"workspace", "create", "--name", "text fixture", "--slug", "text-fixture"},
			method: "POST", path: "/api/workspaces", flag: "context", field: "context",
			stdin: true,
		},
		{
			name: "workspace update description", args: []string{"workspace", "update", issueID, "--name", "text fixture"},
			method: "PATCH", path: "/api/workspaces/" + issueID, flag: "description", field: "description",
			stdin: true,
		},
		{
			name: "workspace update context", args: []string{"workspace", "update", issueID, "--name", "text fixture"},
			method: "PATCH", path: "/api/workspaces/" + issueID, flag: "context", field: "context",
			stdin: true,
		},
	}
	type textCase struct {
		name      string
		flags     []string
		stdin     string
		want      string
		absent    bool
		wantError string
		apiError  bool
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			registered, _, err := rootCmd.Find(command.args)
			if err != nil {
				t.Fatal(err)
			}
			if got := registered.Flags().Lookup(command.flag+"-stdin") != nil; got != command.stdin {
				t.Fatalf("stdin registration = %v, want %v", got, command.stdin)
			}
			if got := registered.Flags().Lookup(command.flag+"-file") != nil; got != command.file {
				t.Fatalf("file registration = %v, want %v", got, command.file)
			}
			cases := []textCase{
				{name: "control", flags: []string{"--" + command.flag, "Plain UTF8 中文 Русский"}, want: "Plain UTF8 中文 Русский"},
				{name: "literal", flags: []string{"--" + command.flag, literal}, want: literal},
				{name: "actual whitespace", flags: []string{"--" + command.flag, "line1\nline2\r\nline3\tend\r"}, want: "line1\nline2\r\nline3\tend\r"},
				{name: "mixed", flags: []string{"--" + command.flag, mixed}, want: mixed},
				{name: "consecutive backslashes", flags: []string{"--" + command.flag, `one\ two\\ three\\\ four\\\\ trailing\`}, want: `one\ two\\ three\\\ four\\\\ trailing\`},
				{name: "omitted", absent: !command.omittedPresent},
				{name: "explicit empty", flags: []string{"--" + command.flag, ""}, absent: command.emptyAbsent},
				{name: "API error", flags: []string{"--" + command.flag, literal}, want: literal, wantError: "synthetic rejection", apiError: true},
			}
			if command.required {
				cases[5].wantError = "--" + command.flag
				cases[6].wantError = "--" + command.flag
			}
			if command.name == "user profile" {
				cases[5].wantError = "nothing to update"
				cases = append(cases,
					textCase{name: "clear", flags: []string{"--clear"}},
					textCase{name: "clear conflicts with description", flags: []string{"--clear", "--description", literal}, wantError: "--clear cannot be combined"},
				)
			}
			if command.stdin {
				cases = append(cases,
					textCase{name: "stdin", flags: []string{"--" + command.flag + "-stdin"}, stdin: mixed, want: strings.TrimSuffix(mixed, "\n")},
					textCase{name: "empty stdin", flags: []string{"--" + command.flag + "-stdin"}, wantError: "is empty"},
					textCase{name: "inline and stdin conflict", flags: []string{"--" + command.flag, literal, "--" + command.flag + "-stdin"}, stdin: mixed, wantError: "mutually exclusive"},
				)
			}
			if command.file {
				cases = append(cases,
					textCase{name: "file", flags: []string{"--" + command.flag + "-file", "body.txt"}, want: strings.TrimSuffix(mixed, "\n")},
					textCase{name: "empty inline and file", flags: []string{"--" + command.flag, "", "--" + command.flag + "-file", "body.txt"}, want: strings.TrimSuffix(mixed, "\n")},
					textCase{name: "empty file", flags: []string{"--" + command.flag + "-file", "empty.txt"}, wantError: "is empty"},
					textCase{name: "missing file", flags: []string{"--" + command.flag + "-file", "missing.txt"}, wantError: "read file"},
					textCase{name: "directory file", flags: []string{"--" + command.flag + "-file", "."}, wantError: "read file"},
					textCase{name: "inline and file conflict", flags: []string{"--" + command.flag, literal, "--" + command.flag + "-file", "body.txt"}, wantError: "mutually exclusive"},
					textCase{name: "external file", flags: []string{"--" + command.flag + "-file", "../outside.txt"}, wantError: "outside the current working directory"},
					textCase{name: "external symlink", flags: []string{"--" + command.flag + "-file", "link.txt"}, wantError: "outside the current working directory"},
				)
			}
			if command.file && command.stdin {
				cases = append(cases, textCase{
					name: "stdin and file conflict", flags: []string{"--" + command.flag + "-stdin", "--" + command.flag + "-file", "body.txt"},
					stdin: mixed, wantError: "mutually exclusive",
				})
			}
			if command.file && registered.Flags().Lookup("allow-external-file") != nil {
				cases = append(cases, textCase{
					name: "external file allowed", flags: []string{"--" + command.flag + "-file", "../outside.txt", "--allow-external-file"},
					want: strings.TrimSuffix(mixed, "\n"),
				})
			}
			if strings.HasPrefix(command.name, "workspace create") {
				cases = append(cases, textCase{
					name: "dual stdin", flags: []string{"--description-stdin", "--context-stdin"},
					stdin: mixed, wantError: "cannot be combined",
				})
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					var mu sync.Mutex
					var requests int
					var body map[string]any
					var raw []byte
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						requests++
						if r.Method != command.method || r.URL.Path != command.path {
							t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
							http.NotFound(w, r)
							return
						}
						var readErr error
						raw, readErr = io.ReadAll(r.Body)
						if readErr != nil {
							t.Error(readErr)
						}
						if err := json.Unmarshal(raw, &body); err != nil {
							t.Error(err)
						}
						w.Header().Set("Content-Type", "application/json")
						if tc.apiError {
							w.WriteHeader(http.StatusBadRequest)
							_, _ = w.Write([]byte(`{"error":"synthetic rejection"}`))
							return
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "TEST-1"})
					}))
					defer server.Close()
					parent := t.TempDir()
					dir := filepath.Join(parent, "run")
					if err := os.Mkdir(dir, 0o700); err != nil {
						t.Fatal(err)
					}
					outside := filepath.Join(parent, "outside.txt")
					if err := os.WriteFile(outside, []byte(mixed), 0o600); err != nil {
						t.Fatal(err)
					}
					if tc.name == "external symlink" {
						if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
							t.Skipf("symlinks unavailable: %v", err)
						}
					}
					if err := os.WriteFile(filepath.Join(dir, "body.txt"), []byte(mixed), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0o600); err != nil {
						t.Fatal(err)
					}
					argv := append(append([]string{}, command.args...), tc.flags...)
					argv = append(argv, "--output", "json")
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					child := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestIssueAttachmentArgvProcess$", "--"}, argv...)...)
					child.Dir = dir
					for _, entry := range os.Environ() {
						if !strings.HasPrefix(entry, "MULTICA_") {
							child.Env = append(child.Env, entry)
						}
					}
					child.Env = append(child.Env, "MULTICA_TEST_ATTACHMENT_ARGV=1", "MULTICA_TEST_ATTACHMENT_SERVER="+server.URL)
					child.Stdin = strings.NewReader(tc.stdin)
					var stderr bytes.Buffer
					child.Stderr = &stderr
					output, runErr := child.Output()
					mu.Lock()
					defer mu.Unlock()
					t.Logf("argv=%q endpoint=%s %s raw=%s decoded=%#v expected=%q stdout=%s stderr=%s", argv, command.method, command.path, raw, body[command.field], tc.want, output, stderr.String())
					if tc.wantError != "" && !tc.apiError {
						if runErr == nil || !strings.Contains(string(output)+stderr.String(), tc.wantError) {
							t.Fatalf("CLI error = %v, want %q", runErr, tc.wantError)
						}
						if requests != 0 {
							t.Fatalf("invalid source made %d requests", requests)
						}
						return
					}
					if tc.apiError {
						if runErr == nil || !strings.Contains(string(output)+stderr.String(), tc.wantError) {
							t.Fatalf("CLI error = %v, want API error %q", runErr, tc.wantError)
						}
					} else if runErr != nil {
						t.Fatalf("CLI error: %v", runErr)
					}
					if requests != 1 {
						t.Fatalf("requests = %d, want 1", requests)
					}
					value, present := body[command.field]
					if present == tc.absent || (present && value != tc.want) {
						t.Errorf("serialized %s = %q (present=%v), want %q (present=%v)", command.field, value, present, tc.want, !tc.absent)
					}
					if command.name == "issue create" {
						if _, present := body["allow_duplicate"]; present {
							t.Error("default issue creation bypassed the duplicate guard")
						}
					}
					if !tc.apiError {
						var printed map[string]any
						if err := json.NewDecoder(bytes.NewReader(output)).Decode(&printed); err != nil || printed["id"] != issueID {
							t.Errorf("normal JSON output = %s, decode error = %v", output, err)
						}
					}
				})
			}
		})
	}
}
