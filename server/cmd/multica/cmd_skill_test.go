package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// captureStdout redirects os.Stdout through a pipe for the duration of fn and
// returns everything written to it, alongside fn's error. Commands that print
// JSON/table output directly to os.Stdout (rather than cobra's out writer) are
// asserted through this helper. Shared across cmd_*_test.go in this package.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	outCh := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(r)
		outCh <- string(buf)
	}()
	runErr := fn()
	os.Stdout = orig
	_ = w.Close()
	out := <-outCh
	_ = r.Close()
	return out, runErr
}

func newSkillImportTestCmd(input string, output *bytes.Buffer, stderr *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{Use: "import"}
	cmd.Flags().String("url", "", "")
	cmd.Flags().String("gitee-token", "", "")
	cmd.Flags().Bool("overwrite", false, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(output)
	cmd.SetErr(stderr)
	return cmd
}

func TestRunSkillImportPromptsOnConflictAndOverwrites(t *testing.T) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/import" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requests = append(requests, body)
		w.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{
				"error":       "a skill with this name already exists",
				"name":        "Review Helper",
				"description": "Imported version",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "skill-1",
			"name": "Review Helper",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	cmd := newSkillImportTestCmd("overwrite\n", &stdout, &stderr)
	if err := cmd.Flags().Set("url", "https://gitee.com/acme/review-helper"); err != nil {
		t.Fatalf("set url: %v", err)
	}

	if err := runSkillImport(cmd, nil); err != nil {
		t.Fatalf("runSkillImport: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	if _, ok := requests[0]["overwrite"]; ok {
		t.Fatalf("first request unexpectedly had overwrite: %+v", requests[0])
	}
	if requests[1]["overwrite"] != true {
		t.Fatalf("second request overwrite = %#v, want true", requests[1]["overwrite"])
	}
	if !strings.Contains(stderr.String(), `Skill "Review Helper" already exists.`) {
		t.Fatalf("prompt missing conflict name: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"id": "skill-1"`) {
		t.Fatalf("stdout missing imported skill JSON: %s", stdout.String())
	}
}

func TestRunSkillImportPromptsOnConflictAndSkips(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/import" {
			http.NotFound(w, r)
			return
		}
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"error":       "a skill with this name already exists",
			"name":        "Review Helper",
			"description": "Imported version",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	cmd := newSkillImportTestCmd("skip\n", &stdout, &stderr)
	if err := cmd.Flags().Set("url", "https://gitee.com/acme/review-helper"); err != nil {
		t.Fatalf("set url: %v", err)
	}

	if err := runSkillImport(cmd, nil); err != nil {
		t.Fatalf("runSkillImport: %v", err)
	}

	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
	if !strings.Contains(stderr.String(), `Skill "Review Helper" already exists.`) {
		t.Fatalf("prompt missing conflict name: %s", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, stdout.String())
	}
	if out["skipped"] != true || out["name"] != "Review Helper" {
		t.Fatalf("unexpected skip JSON: %+v", out)
	}
}

func TestRunSkillImportOverwriteFlagSkipsPrompt(t *testing.T) {
	var request map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/import" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "skill-1",
			"name": "Review Helper",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	cmd := newSkillImportTestCmd("", &stdout, &stderr)
	if err := cmd.Flags().Set("url", "https://gitee.com/acme/review-helper"); err != nil {
		t.Fatalf("set url: %v", err)
	}
	if err := cmd.Flags().Set("overwrite", "true"); err != nil {
		t.Fatalf("set overwrite: %v", err)
	}

	if err := runSkillImport(cmd, nil); err != nil {
		t.Fatalf("runSkillImport: %v", err)
	}

	if request["overwrite"] != true {
		t.Fatalf("overwrite = %#v, want true", request["overwrite"])
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected prompt output: %s", stderr.String())
	}
}
