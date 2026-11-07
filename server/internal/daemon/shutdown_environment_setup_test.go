package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHandleTask_ShutdownEnvironmentSetup(t *testing.T) {
	for _, phase := range []string{"prepare", "reuse"} {
		t.Run(phase, func(t *testing.T) {
			d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
			defer cleanup()
			task := leaderReuseTestTask("setup-waiter")
			failures := make(chan map[string]string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/fail") {
					var payload map[string]string
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					failures <- payload
				} else if strings.HasSuffix(r.URL.Path, "/status") {
					_, _ = w.Write([]byte(`{"status":"running"}`))
				}
			}))
			defer srv.Close()
			d.client = NewClient(srv.URL)
			if phase == "reuse" {
				prior, err := d.runTask(context.Background(), leaderReuseTestTask("prior-task"), "claude", 0, d.logger)
				if err != nil {
					t.Fatal(err)
				}
				task.PriorWorkDir, task.PriorSessionID = prior.WorkDir, prior.SessionID
			}
			before, _ := os.ReadFile(argsFile)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Cancel at the existing helper-command seam. runTask still creates
			// the real prepare/reuse error wrapper; no helper or agent is started.
			d.executionEnvironmentCommand = func() ([]string, error) {
				cancel()
				return nil, ctx.Err()
			}
			d.runner = taskRunnerFunc(d.runTask)
			d.handleTask(ctx, task, 0)
			select {
			case payload := <-failures:
				if payload["failure_reason"] != "runtime_recovery" || !strings.Contains(payload["error"], phase+" execution environment: context canceled") {
					t.Fatalf("%s shutdown callback = %v", phase, payload)
				}
			default:
				t.Fatalf("missing %s shutdown callback", phase)
			}
			after, _ := os.ReadFile(argsFile)
			if !bytes.Equal(before, after) {
				t.Fatal("shutdown setup launched the fake agent")
			}
		})
	}
}
