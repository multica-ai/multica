package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	rotateURLAutopilotID = "11111111-1111-4111-8111-111111111111"
	rotateURLTriggerID   = "22222222-2222-4222-8222-222222222222"
	rotateURLWorkspace   = "33333333-3333-4333-8333-333333333333"
)

// The subprocess runs the registered Cobra command and main's error/exit handling.
// TestMain keeps its profile and filesystem access inside the existing scratch home.
func TestAutopilotRotateURLCLIHelper(t *testing.T) {
	if os.Getenv("MULTICA_TEST_ROTATE_URL_HELPER") != "1" {
		return
	}
	t.Setenv("MULTICA_TOKEN", "mat_rotate-url-test")
	t.Setenv("MULTICA_HTTP_TIMEOUT", "1s")
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"multica"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing CLI arguments")
}

func TestAutopilotRotateURLCLI(t *testing.T) {
	t.Setenv("MULTICA_HTTP_TIMEOUT", "1s")
	for _, tc := range []struct {
		name       string
		answer     string
		yes        bool
		slow       bool
		prefix     bool
		output     string
		failure    string
		exit       int
		wantWrites int
	}{
		{name: "slow confirmation", answer: "y\n", slow: true, output: "json", wantWrites: 1},
		{name: "resolved prefixes after slow confirmation", answer: "yes\n", slow: true, prefix: true, output: "json", wantWrites: 1},
		{name: "yes flag", yes: true, output: "json", wantWrites: 1},
		{name: "fast confirmation table", answer: "YES\n", output: "table", wantWrites: 1},
		{name: "decline", answer: "n\n", output: "json"},
		{name: "EOF", output: "json"},
		{name: "permission denied", yes: true, output: "json", failure: "forbidden", exit: cli.ExitAuth, wantWrites: 1},
		{name: "server failure", yes: true, output: "json", failure: "server", exit: 1, wantWrites: 1},
		{name: "truncated JSON", yes: true, output: "json", failure: "JSON", exit: cli.ExitNetwork, wantWrites: 1},
		{name: "write timeout", yes: true, output: "json", failure: "timeout", exit: 2, wantWrites: 1},
		{name: "autopilot resolution failure", yes: true, prefix: true, output: "json", failure: "autopilot", exit: cli.ExitAuth},
		{name: "trigger resolution failure", yes: true, prefix: true, output: "json", failure: "trigger", exit: cli.ExitAuth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []string
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.Method+" "+r.URL.Path)
				if r.Method == http.MethodPost {
					writes++
				}
				mu.Unlock()
				if r.Header.Get("Authorization") != "Bearer mat_rotate-url-test" ||
					r.Header.Get("X-Workspace-ID") != rotateURLWorkspace {
					t.Error("request lost fixture authorization or workspace")
				}
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/autopilots":
					if r.URL.Query().Get("workspace_id") != rotateURLWorkspace {
						t.Error("prefix lookup lost workspace query")
					}
					if tc.failure == "autopilot" {
						http.Error(w, "cannot resolve autopilot", http.StatusForbidden)
						return
					}
					fmt.Fprintf(w, `{"autopilots":[{"id":%q}],"total":1,"has_more":false}`, rotateURLAutopilotID)
				case r.Method == http.MethodGet && r.URL.Path == "/api/autopilots/"+rotateURLAutopilotID:
					if tc.failure == "trigger" {
						http.Error(w, "cannot resolve trigger", http.StatusForbidden)
						return
					}
					fmt.Fprintf(w, `{"triggers":[{"id":%q,"kind":"webhook"}]}`, rotateURLTriggerID)
				case r.Method == http.MethodPost && r.URL.Path == "/api/autopilots/"+rotateURLAutopilotID+"/triggers/"+rotateURLTriggerID+"/rotate-webhook-token":
					switch tc.failure {
					case "forbidden":
						http.Error(w, "rotation forbidden", http.StatusForbidden)
					case "server":
						http.Error(w, "rotation failed", http.StatusInternalServerError)
					case "JSON":
						fmt.Fprint(w, "{")
					case "timeout":
						select {
						case <-r.Context().Done():
						case <-time.After(4 * time.Second):
						}
					default:
						fmt.Fprintf(w, `{"id":%q,"webhook_url":"https://example.invalid/synthetic-webhook"}`, rotateURLTriggerID)
					}
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			autopilot, trigger := rotateURLAutopilotID, rotateURLTriggerID
			if tc.prefix {
				autopilot, trigger = autopilot[:8], trigger[:8]
			}
			args := []string{"-test.run=^TestAutopilotRotateURLCLIHelper$", "--",
				"--server-url", srv.URL, "--workspace-id", rotateURLWorkspace,
				"autopilot", "trigger-rotate-url", autopilot, trigger, "--output", tc.output}
			if tc.yes {
				args = append(args, "--yes")
			}
			cmd := exec.Command(os.Args[0], args...)
			// Child main exits directly on errors, so let the parent test own
			// the scratch homes created by the child's original TestMain.
			childTemp := t.TempDir()
			cmd.Env = append(os.Environ(), "MULTICA_TEST_ROTATE_URL_HELPER=1",
				"TMPDIR="+childTemp, "TMP="+childTemp, "TEMP="+childTemp)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			var out, diagnostic bytes.Buffer
			cmd.Stdout = &out
			prompt := make(chan struct{}, 1)
			readDone := make(chan struct{})
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			go func() {
				defer close(readDone)
				scanner := bufio.NewScanner(stderr)
				for scanner.Scan() {
					line := scanner.Text()
					fmt.Fprintln(&diagnostic, line)
					if strings.Contains(line, "[y/N]") {
						prompt <- struct{}{}
					}
				}
			}()
			if !tc.yes {
				select {
				case <-prompt:
					if tc.slow {
						// Start a real APIContext after observing the prompt. Its
						// deadline expires after any pre-prompt request context.
						ctx, cancel := cli.APIContext(context.Background())
						deadline, _ := ctx.Deadline()
						<-ctx.Done()
						t.Logf("prompt observed; real deadline %s completed with %v", deadline.Format(time.RFC3339Nano), ctx.Err())
						cancel()
						mu.Lock()
						if writes != 0 {
							t.Error("rotation occurred before confirmation")
						}
						mu.Unlock()
					}
					if tc.answer != "" {
						if _, err := fmt.Fprint(stdin, tc.answer); err != nil {
							t.Error(err)
						}
					}
				case <-readDone:
					t.Error("CLI exited before its confirmation prompt")
				case <-time.After(15 * time.Second):
					t.Error("CLI did not emit its confirmation prompt")
				}
			}
			_ = stdin.Close()
			<-readDone
			err = cmd.Wait()
			exit := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				exit = exitErr.ExitCode()
			}
			if exit != tc.exit {
				t.Errorf("exit = %d, want %d; stderr = %s", exit, tc.exit, diagnostic.String())
			}
			mu.Lock()
			actualWrites := writes
			actualRequests := append([]string(nil), requests...)
			mu.Unlock()
			t.Logf("requests = %v; exit = %d; stdout = %q; stderr = %q", actualRequests, exit, out.String(), diagnostic.String())
			if actualWrites != tc.wantWrites {
				t.Errorf("POST count = %d, want %d", actualWrites, tc.wantWrites)
			}
			wantRequests := []string{}
			if tc.prefix {
				wantRequests = append(wantRequests, "GET /api/autopilots")
				if tc.failure != "autopilot" {
					wantRequests = append(wantRequests, "GET /api/autopilots/"+rotateURLAutopilotID)
				}
			}
			if tc.wantWrites > 0 {
				wantRequests = append(wantRequests, "POST /api/autopilots/"+rotateURLAutopilotID+"/triggers/"+rotateURLTriggerID+"/rotate-webhook-token")
			}
			if strings.Join(actualRequests, "\n") != strings.Join(wantRequests, "\n") {
				t.Errorf("requests = %v, want %v", actualRequests, wantRequests)
			}
			switch {
			case tc.failure != "":
				if out.Len() != 0 || diagnostic.Len() == 0 {
					t.Errorf("failure output stdout=%q stderr=%q", out.String(), diagnostic.String())
				}
			case tc.wantWrites == 0:
				if out.Len() != 0 || !strings.Contains(diagnostic.String(), "Aborted.") {
					t.Errorf("abort output stdout=%q stderr=%q", out.String(), diagnostic.String())
				}
			case tc.output == "json":
				var result map[string]any
				if err := json.Unmarshal(out.Bytes(), &result); err != nil || result["id"] != rotateURLTriggerID {
					t.Errorf("rotation JSON = %q, error = %v", out.String(), err)
				}
			default:
				for _, want := range []string{"Webhook URL rotated for trigger " + rotateURLTriggerID, "https://example.invalid/synthetic-webhook"} {
					if !strings.Contains(out.String(), want) {
						t.Errorf("table output %q missing %q", out.String(), want)
					}
				}
			}
		})
	}
}
