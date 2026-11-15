package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newEnvironmentTestCmd(flags map[string]string) *cobra.Command {
	cmd := &cobra.Command{Use: "env-test"}
	cmd.Flags().Bool("wait", true, "")
	cmd.Flags().String("output", "table", "")
	for k, v := range flags {
		_ = cmd.Flags().Set(k, v)
	}
	return cmd
}

func fakeEnvironmentDaemon(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	t.Setenv("MULTICA_DAEMON_PORT", u.Port())
	t.Setenv("MULTICA_TOKEN", "mat_task")
}

func TestEnvironmentStartSendsTheTaskContextAndPrintsTheURL(t *testing.T) {
	var got map[string]any
	fakeEnvironmentDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/environments/start" || r.Header.Get("Authorization") != "Bearer mat_task" {
			t.Errorf("request = %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"environment": map[string]any{
			"name": "web", "url": "http://localhost:41000", "port": 41000, "state": "running", "log_path": "/tmp/web.log",
		}})
	})
	t.Setenv("PATH", "/opt/node/bin:/usr/bin")
	wd, _ := os.Getwd()

	out, err := captureStdout(t, func() error { return runEnvironmentStart(newEnvironmentTestCmd(nil), []string{"web"}) })
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if strings.TrimSpace(out) != "http://localhost:41000" {
		t.Fatalf("stdout = %q, want the URL alone", out)
	}
	if got["name"] != "web" || got["dir"] != wd || got["path"] != "/opt/node/bin:/usr/bin" || got["wait"] != true {
		t.Fatalf("request body = %v", got)
	}
}

func TestEnvironmentStartFailsWhenTheEnvironmentExited(t *testing.T) {
	fakeEnvironmentDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"environment": map[string]any{
			"name": "web", "state": "exited", "detail": "exit status 1", "log_path": "/tmp/web.log",
		}})
	})
	_, err := captureStdout(t, func() error { return runEnvironmentStart(newEnvironmentTestCmd(nil), []string{"web"}) })
	if err == nil || !strings.Contains(err.Error(), "exited (exit status 1)") || !strings.Contains(err.Error(), "/tmp/web.log") {
		t.Fatalf("err = %v, want the exit status and log path", err)
	}
}

func TestEnvironmentLogTailReadsTheLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.log")
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := logTail(path, 3), "line 28\nline 29\nline 30"; got != want {
		t.Fatalf("logTail = %q, want %q", got, want)
	}
	if got := logTail(filepath.Join(t.TempDir(), "missing.log"), 3); got != "" {
		t.Fatalf("logTail of a missing log = %q, want empty", got)
	}
}

func TestEnvironmentCommandsExplainAnOldDaemonAndDaemonErrors(t *testing.T) {
	fakeEnvironmentDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/environments/stop" {
			http.Error(w, `no running environment named "web" on this issue`, http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	})
	err := runEnvironmentList(newEnvironmentTestCmd(nil), nil)
	if err == nil || !strings.Contains(err.Error(), "multica update") {
		t.Fatalf("list against an old daemon = %v", err)
	}
	err = runEnvironmentStop(newEnvironmentTestCmd(nil), []string{"web"})
	if err == nil || err.Error() != `no running environment named "web" on this issue` {
		t.Fatalf("stop = %v, want the daemon's message", err)
	}
}

func TestEnvironmentCommandsNeedATask(t *testing.T) {
	t.Setenv("MULTICA_DAEMON_PORT", "")
	if err := runEnvironmentList(newEnvironmentTestCmd(nil), nil); err == nil || !strings.Contains(err.Error(), "inside a daemon task") {
		t.Fatalf("err = %v", err)
	}
}
