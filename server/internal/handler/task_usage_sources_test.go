package handler

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestTaskUsageSourcesRoundTrip(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "UsageSources", []byte("[]"))
	issueID := dbfx.Issue(t, "usage sources")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issueID, "status": "completed"})
	dbfx.Cleanup(t, "DELETE FROM task_usage WHERE task_id = $1", taskID)

	report := func(t *testing.T, body any, status int) {
		t.Helper()
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/usage", body, testWorkspaceID, "usage-sources")
		req = withURLParam(req, "taskId", taskID)
		testutil.Call(t, testHandler.ReportTaskUsage, req).Want(status)
	}
	read := func(t *testing.T) AgentTaskResponse {
		t.Helper()
		req := withURLParam(newRequest(http.MethodGet, "/api/issues/"+issueID+"/task-runs", nil), "id", issueID)
		var rows []AgentTaskResponse
		testutil.Call(t, testHandler.ListTasksByIssue, req).Want(http.StatusOK).JSON(&rows)
		if len(rows) != 1 || rows[0].ID != taskID {
			t.Fatalf("unexpected runs: %+v", rows)
		}
		return rows[0]
	}
	if got := read(t); len(got.UsageSources) != 0 || len(got.Usage) != 0 {
		t.Fatalf("legacy task fabricated usage: %+v", got)
	}

	for _, sources := range [][]string{{"assistant_fallback"}, {"final_usage"}, {"assistant_fallback", "final_model_usage"}, {"final_model_usage"}} {
		t.Run(sources[0]+"_round_trip", func(t *testing.T) {
			body := map[string]any{"usage_sources": sources, "usage": []TaskUsagePayload{{Provider: "CLAUDE", Model: "claude-sonnet-4-6", InputTokens: 100, OutputTokens: 0}}}
			for i := 0; i < 2; i++ { // Retried reports replace, never add counters.
				report(t, body, http.StatusOK)
				got := read(t)
				if !reflect.DeepEqual(got.UsageSources, sources) {
					t.Fatalf("sources = %v, want %v", got.UsageSources, sources)
				}
				if len(got.Usage) != 1 || got.Usage[0].InputTokens != 100 || got.Usage[0].OutputTokens != 0 || got.Usage[0].Provider != "claude" {
					t.Fatalf("usage = %+v", got.Usage)
				}
			}
		})
	}

	t.Run("rollback_counters_and_sources_together", func(t *testing.T) {
		report(t, map[string]any{"usage_sources": []string{"none"}, "usage": []TaskUsagePayload{{Model: "would-be-written", InputTokens: 9}, {Model: "bad\x00model", InputTokens: 1}}}, http.StatusInternalServerError)
		got := read(t)
		if !reflect.DeepEqual(got.UsageSources, []string{"final_model_usage"}) || len(got.Usage) != 1 || got.Usage[0].Model != "claude-sonnet-4-6" || got.Usage[0].InputTokens != 100 {
			t.Fatalf("partial transaction escaped: %+v", got)
		}
	})
	t.Run("legacy_report_clears_stale_scope", func(t *testing.T) {
		report(t, map[string]any{"usage": []TaskUsagePayload{{Provider: "claude", Model: "claude-sonnet-4-6", InputTokens: 7}}}, http.StatusOK)
		got := read(t)
		if len(got.UsageSources) != 0 || len(got.Usage) != 1 || got.Usage[0].InputTokens != 7 {
			t.Fatalf("legacy report = %+v", got)
		}
	})
	t.Run("corrected_snapshot_removes_old_models", func(t *testing.T) {
		report(t, map[string]any{"usage_sources": []string{"final_model_usage"}, "usage": []TaskUsagePayload{{Provider: "claude", Model: "claude-haiku-4-5", InputTokens: 3}}}, http.StatusOK)
		got := read(t)
		if len(got.Usage) != 1 || got.Usage[0].Model != "claude-haiku-4-5" {
			t.Fatalf("stale models survived: %+v", got.Usage)
		}
	})
	t.Run("explicit_no_counters_survives_without_a_model_row", func(t *testing.T) {
		report(t, map[string]any{"usage_sources": []string{"none"}}, http.StatusOK)
		got := read(t)
		if !reflect.DeepEqual(got.UsageSources, []string{"none"}) || len(got.Usage) != 0 {
			t.Fatalf("no-usage report = %+v", got)
		}
	})
	t.Run("malformed_source_does_not_change_saved_report", func(t *testing.T) {
		report(t, map[string]any{"usage_sources": "final_model_usage"}, http.StatusBadRequest)
		if got := read(t); !reflect.DeepEqual(got.UsageSources, []string{"none"}) {
			t.Fatalf("malformed report changed source: %v", got.UsageSources)
		}
	})
	t.Run("agent_history_preserves_no_counters_evidence", func(t *testing.T) {
		req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks?include_usage=true", nil), "id", agentID)
		var rows []AgentTaskResponse
		testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&rows)
		if len(rows) != 1 || !reflect.DeepEqual(rows[0].UsageSources, []string{"none"}) {
			t.Fatalf("history = %+v", rows)
		}
	})
	t.Run("cross_workspace_cannot_replace_usage", func(t *testing.T) {
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/usage", map[string]any{"usage_sources": []string{"final_model_usage"}}, uuid.NewString(), "foreign-daemon")
		testutil.Call(t, testHandler.ReportTaskUsage, withURLParam(req, "taskId", taskID)).Want(http.StatusNotFound)
		row := read(t)
		if !reflect.DeepEqual(row.UsageSources, []string{"none"}) {
			t.Fatalf("foreign report changed source: %v", row.UsageSources)
		}
	})
}
