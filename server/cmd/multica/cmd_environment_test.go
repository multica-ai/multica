package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
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

func TestEnvironmentUnauthorizedUsesTheFinalRequest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		token      string
		redirect   bool
		locale     string
		wantScoped bool
	}{
		{name: "task credential", token: "mat_synthetic", wantScoped: true},
		{name: "task credential in Chinese", token: "mat_synthetic", locale: "zh_CN.UTF-8", wantScoped: true},
		{name: "other credential", token: "synthetic_other"},
		{name: "cross host redirect", token: "mat_synthetic", redirect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const message = "environment commands need the credential of a task running in this daemon"
			locale := tc.locale
			if locale == "" {
				locale = "en_US.UTF-8"
			}
			t.Setenv("LC_ALL", locale)
			t.Setenv("MULTICA_DEBUG", "")
			final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.redirect && r.Header.Get("Authorization") != "" {
					t.Error("cross-host redirect retained Authorization")
				}
				http.Error(w, message, http.StatusUnauthorized)
			}))
			defer final.Close()
			fakeEnvironmentDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+tc.token {
					t.Errorf("initial Authorization = %q", r.Header.Get("Authorization"))
				}
				if tc.redirect {
					http.Redirect(w, r, strings.Replace(final.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
					return
				}
				http.Error(w, message, http.StatusUnauthorized)
			})
			t.Setenv("MULTICA_TOKEN", tc.token)
			err := runEnvironmentList(newEnvironmentTestCmd(nil), nil)
			var httpErr *cli.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusUnauthorized || httpErr.TaskScoped != tc.wantScoped {
				t.Fatalf("error = %#v, want HTTP401 TaskScoped=%v", err, tc.wantScoped)
			}
			if cli.ExitCodeFor(err) != cli.ExitAuth {
				t.Errorf("exit = %d, want auth", cli.ExitCodeFor(err))
			}
			formatted := cli.FormatError(err, false)
			stop := cli.TaskTokenRejectedMessage()
			if !strings.Contains(formatted, message) || strings.Contains(formatted, "multica login") || strings.Contains(formatted, stop) != tc.wantScoped {
				t.Errorf("formatted error = %q", formatted)
			}
		})
	}
}

func TestEnvironmentUnauthorizedSharedCallers(t *testing.T) {
	fakeEnvironmentDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "active task credential required", http.StatusUnauthorized)
	})
	for name, run := range map[string]func(*cobra.Command, []string) error{
		"start": runEnvironmentStart,
		"stop":  runEnvironmentStop,
		"list":  runEnvironmentList,
	} {
		t.Run(name, func(t *testing.T) {
			out, err := captureStdout(t, func() error {
				return run(newEnvironmentTestCmd(map[string]string{"output": "json"}), []string{"web"})
			})
			if cli.ExitCodeFor(err) != cli.ExitAuth || out != "" {
				t.Errorf("stdout=%q error=%v exit=%d", out, err, cli.ExitCodeFor(err))
			}
		})
	}
}

func TestEnvironmentOtherStatusMessages(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fakeEnvironmentDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "daemon message", status)
			})
			err := runEnvironmentList(newEnvironmentTestCmd(nil), nil)
			if err == nil || err.Error() != "daemon message" || cli.ExitCodeFor(err) != cli.ExitGeneric {
				t.Errorf("err=%v exit=%d", err, cli.ExitCodeFor(err))
			}
		})
	}
}

func TestEnvironmentResponseFailures(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		t.Setenv("MULTICA_DAEMON_PORT", "1")
		t.Setenv("MULTICA_TOKEN", "")
		err := runEnvironmentList(newEnvironmentTestCmd(nil), nil)
		if err == nil || !strings.Contains(err.Error(), "active task credential") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("encode", func(t *testing.T) {
		t.Setenv("MULTICA_DAEMON_PORT", "1")
		t.Setenv("MULTICA_TOKEN", "mat_synthetic")
		err := callDaemonEnvironment(context.Background(), "/environments/list", make(chan int), nil, time.Second)
		if err == nil || !strings.Contains(err.Error(), "encode request") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		t.Setenv("MULTICA_DAEMON_PORT", "1")
		t.Setenv("MULTICA_TOKEN", "mat_synthetic")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := callDaemonEnvironment(ctx, "/environments/list", map[string]any{}, nil, time.Second)
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "connect to daemon") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("network", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		u, _ := url.Parse(server.URL)
		server.Close()
		t.Setenv("MULTICA_DAEMON_PORT", u.Port())
		t.Setenv("MULTICA_TOKEN", "mat_synthetic")
		err := runEnvironmentList(newEnvironmentTestCmd(nil), nil)
		if err == nil || !strings.Contains(err.Error(), "connect to daemon") || cli.ExitCodeFor(err) != cli.ExitGeneric {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("truncated body", func(t *testing.T) {
		fakeEnvironmentDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "100")
			fmt.Fprint(w, "short")
		})
		err := runEnvironmentList(newEnvironmentTestCmd(nil), nil)
		if err == nil || !strings.Contains(err.Error(), "read daemon response") || cli.ExitCodeFor(err) != cli.ExitGeneric {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid JSON", func(t *testing.T) {
		fakeEnvironmentDaemon(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "invalid")
		})
		err := runEnvironmentList(newEnvironmentTestCmd(nil), nil)
		if err == nil || !strings.Contains(err.Error(), "parse daemon response") || cli.ExitCodeFor(err) != cli.ExitGeneric {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestEnvironmentStopPrintsSuccessfulJSON(t *testing.T) {
	fakeEnvironmentDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/environments/stop" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"environment":{"name":"web","state":"stopped"}}`)
	})
	out, err := captureStdout(t, func() error {
		return runEnvironmentStop(newEnvironmentTestCmd(map[string]string{"output": "json"}), []string{"web"})
	})
	var env daemonEnvironment
	if err != nil || json.Unmarshal([]byte(out), &env) != nil || env.Name != "web" || env.State != "stopped" {
		t.Fatalf("stdout=%q error=%v", out, err)
	}
}
