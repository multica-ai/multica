package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestFreshRetryUsageSources(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name               string
		first, retry, want []string
		err                error
	}{
		{name: "partial_then_final", first: []string{"assistant_fallback"}, retry: []string{"final_model_usage"}, want: []string{"assistant_fallback", "final_model_usage"}},
		{name: "missing_then_final", first: []string{"none"}, retry: []string{"final_model_usage"}, want: []string{"none", "final_model_usage"}},
		{name: "unknown_then_final", retry: []string{"final_model_usage"}, want: []string{"unknown", "final_model_usage"}},
		{name: "final_then_unknown", first: []string{"final_model_usage"}, want: []string{"final_model_usage", "unknown"}},
		{name: "different_final_scopes", first: []string{"final_usage"}, retry: []string{"final_model_usage"}, want: []string{"final_usage", "final_model_usage"}},
		{name: "same_source", first: []string{"final_model_usage"}, retry: []string{"final_model_usage"}, want: []string{"final_model_usage"}},
		{name: "uninstrumented"},
		{name: "retry_never_started", first: []string{"assistant_fallback"}, retry: []string{"none"}, want: []string{"assistant_fallback"}, err: errors.New("start failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, retry := range []agent.Result{{Status: "completed"}, {Status: "failed", SessionID: "new"}, {Status: "failed"}} {
				first := agent.Result{Status: "failed", UsageSources: tt.first}
				retry.UsageSources = tt.retry
				firstUsage := map[string]agent.TokenUsage{"model": {InputTokens: 10}}
				retry.Usage = map[string]agent.TokenUsage{"model": {InputTokens: 20}}
				got, _ := reconcileFreshRetryResult(first, firstUsage, 1, retry, 2, tt.err)
				if !reflect.DeepEqual(got.UsageSources, tt.want) {
					t.Fatalf("retry %s/%s: sources %v, want %v", retry.Status, retry.SessionID, got.UsageSources, tt.want)
				}
				wantTokens := int64(30)
				if tt.err != nil {
					wantTokens = 10
				}
				if got.Usage["model"].InputTokens != wantTokens {
					t.Fatalf("counters = %v", got.Usage)
				}
			}
		})
	}
}

// Exercise runner -> handleTask -> HTTP, including an explicit no-statistics
// report with no model rows, before cancellation discards the terminal result.
func TestHandleTaskReportsUsageSourcesWithoutCounters(t *testing.T) {
	t.Parallel()
	reports := make(chan []string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/usage") {
			var body struct {
				Usage   []TaskUsageEntry `json:"usage"`
				Sources []string         `json:"usage_sources"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Usage) != 0 {
				t.Errorf("invented model rows: %v", body.Usage)
			}
			reports <- body.Sources
		}
		if strings.HasSuffix(r.URL.Path, "/status") {
			_, _ = w.Write([]byte(`{"status":"cancelled"}`))
		}
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), workspaces: make(map[string]*workspaceState), runtimeIndex: map[string]Runtime{"rt": {ID: "rt", Provider: "claude"}}, cancelPollInterval: time.Hour}
	d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
		return TaskResult{Status: "completed", UsageSources: []string{"none"}}, nil
	})
	d.handleTask(context.Background(), Task{ID: "task", RuntimeID: "rt", IssueID: "issue", Agent: &AgentData{Name: "fixture"}}, 0)
	select {
	case got := <-reports:
		if !reflect.DeepEqual(got, []string{"none"}) {
			t.Fatalf("sources = %v", got)
		}
	default:
		t.Fatal("explicit no-statistics report was dropped")
	}
}
