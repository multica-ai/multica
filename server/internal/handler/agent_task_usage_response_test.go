package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestListAgentTasksHydratesUsage pins the JSON contract used by
// `multica agent tasks --output json`: usage is returned at the stored
// (provider, model) grain, only for tasks owned by the requested agent, and
// remains absent when a task has no recorded usage.
func TestListAgentTasksHydratesUsage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "AgentTaskUsage", []byte("[]"))
	otherAgentID := createHandlerTestAgent(t, "AgentTaskUsageOther", []byte("[]"))

	newTask := func(agentID string) string {
		return dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": handlerTestRuntimeID(t),
			"status":     "completed",
		})
	}

	usageTask := newTask(agentID)
	noUsageTask := newTask(agentID)
	otherAgentTask := newTask(otherAgentID)

	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":            usageTask,
		"provider":           "anthropic",
		"model":              "claude-opus-5",
		"input_tokens":       96000,
		"output_tokens":      34000,
		"cache_read_tokens":  712000,
		"cache_write_tokens": 50000,
		"cost_usd_ticks":     nil,
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":            usageTask,
		"provider":           "openai",
		"model":              "gpt-5.6-terra",
		"input_tokens":       31000,
		"output_tokens":      12000,
		"cache_read_tokens":  158000,
		"cache_write_tokens": 11000,
		"cost_usd_ticks":     int64(3310000000),
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":            otherAgentTask,
		"provider":           "openai",
		"model":              "other-agent-model",
		"input_tokens":       1,
		"output_tokens":      2,
		"cache_read_tokens":  3,
		"cache_write_tokens": 4,
		"cost_usd_ticks":     5,
	})

	usageRows, err := testHandler.Queries.ListAgentTaskUsage(ctx, db.ListAgentTaskUsageParams{
		AgentID: parseUUID(agentID),
		TaskIds: []pgtype.UUID{parseUUID(usageTask), parseUUID(noUsageTask)},
	})
	if err != nil {
		t.Fatalf("list agent task usage: %v", err)
	}
	if len(usageRows) != 2 {
		t.Fatalf("agent-scoped usage rows = %d, want 2: %+v", len(usageRows), usageRows)
	}
	for _, row := range usageRows {
		if uuidToString(row.TaskID) != usageTask {
			t.Fatalf("agent-scoped usage leaked task %s, want only %s", uuidToString(row.TaskID), usageTask)
		}
	}

	req := newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks?include_usage=true", nil)
	req = withURLParam(req, "id", agentID)
	var resp []AgentTaskResponse
	w := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&resp)

	byID := make(map[string]AgentTaskResponse, len(resp))
	for _, task := range resp {
		byID[task.ID] = task
	}
	if _, ok := byID[otherAgentTask]; ok {
		t.Fatalf("response leaked task %s from another agent", otherAgentTask)
	}

	withUsage, ok := byID[usageTask]
	if !ok {
		t.Fatalf("usage task %s missing from response", usageTask)
	}
	if len(withUsage.Usage) != 2 {
		t.Fatalf("usage rows = %d, want 2: %+v", len(withUsage.Usage), withUsage.Usage)
	}

	byModel := make(map[string]TaskUsageData, len(withUsage.Usage))
	for _, usage := range withUsage.Usage {
		byModel[usage.Model] = usage
	}
	opus, ok := byModel["claude-opus-5"]
	if !ok {
		t.Fatalf("claude-opus-5 row missing: %+v", withUsage.Usage)
	}
	if opus.Provider != "anthropic" || opus.InputTokens != 96000 ||
		opus.OutputTokens != 34000 || opus.CacheReadTokens != 712000 ||
		opus.CacheWriteTokens != 50000 {
		t.Errorf("unexpected claude-opus-5 usage: %+v", opus)
	}
	if opus.CostUsdTicks != nil {
		t.Errorf("claude-opus-5 cost_usd_ticks = %v, want nil", opus.CostUsdTicks)
	}

	terra, ok := byModel["gpt-5.6-terra"]
	if !ok {
		t.Fatalf("gpt-5.6-terra row missing: %+v", withUsage.Usage)
	}
	if terra.CostUsdTicks == nil || *terra.CostUsdTicks != 3310000000 {
		t.Errorf("gpt-5.6-terra cost_usd_ticks = %v, want 3310000000", terra.CostUsdTicks)
	}

	withoutUsage, ok := byID[noUsageTask]
	if !ok {
		t.Fatalf("no-usage task %s missing from response", noUsageTask)
	}
	if len(withoutUsage.Usage) != 0 {
		t.Errorf("task with no recorded usage has %d rows: %+v", len(withoutUsage.Usage), withoutUsage.Usage)
	}

	var raw []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw task list: %v", err)
	}
	for _, task := range raw {
		if task["id"] != noUsageTask {
			continue
		}
		if _, present := task["usage"]; present {
			t.Errorf("no-usage task serialises a usage key: %v", task["usage"])
		}
	}

	invalidReq := newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks?include_usage=yes", nil)
	invalidReq = withURLParam(invalidReq, "id", agentID)
	invalidResp := testutil.Call(t, testHandler.ListAgentTasks, invalidReq).Want(http.StatusBadRequest)
	if !strings.Contains(invalidResp.Body.String(), "include_usage must be true or false") {
		t.Fatalf("invalid include_usage response = %s", invalidResp.Body.String())
	}
}

func TestListAgentTasksReturnsErrorWhenUsageLoadFails(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "AgentTaskUsageFailure", []byte("[]"))
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "completed",
	})

	lockTx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin controlled task usage query failure: %v", err)
	}
	t.Cleanup(func() { _ = lockTx.Rollback(context.Background()) })
	if _, err := lockTx.Exec(context.Background(), `LOCK TABLE task_usage IN ACCESS EXCLUSIVE MODE`); err != nil {
		_ = lockTx.Rollback(context.Background())
		t.Fatalf("lock task usage for controlled query failure: %v", err)
	}

	defaultReq := newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks", nil)
	defaultReq = withURLParam(defaultReq, "id", agentID)
	var defaultResp []AgentTaskResponse
	testutil.Call(t, testHandler.ListAgentTasks, defaultReq).Want(http.StatusOK).JSON(&defaultResp)
	if len(defaultResp) == 0 {
		t.Fatal("default task history unexpectedly empty")
	}
	for _, task := range defaultResp {
		if len(task.Usage) != 0 {
			t.Fatalf("default task history loaded usage while accounting table was locked: %+v", task.Usage)
		}
	}

	req := newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks?include_usage=true", nil)
	req = withURLParam(req, "id", agentID)
	errorCtx, cancel := context.WithTimeout(req.Context(), 250*time.Millisecond)
	req = req.WithContext(errorCtx)
	w := testutil.Call(t, testHandler.ListAgentTasks, req)
	cancel()
	if err := lockTx.Rollback(context.Background()); err != nil {
		t.Fatalf("release controlled task usage lock: %v", err)
	}

	w.Want(http.StatusInternalServerError)
	if !strings.Contains(w.Body.String(), "failed to list agent task usage") {
		t.Fatalf("usage query failure should not fall back to empty usage: %s", w.Body.String())
	}
}
