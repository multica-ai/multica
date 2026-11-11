package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestClaimTaskManagedMcpHistorySurvivesConfigEdit(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Fatal("explicit isolated database required")
	}
	ctx := context.Background()
	shared := createWorkspaceMcpServerForTest(t, "history-shared", `{"url":"https://synthetic.example/SECRET_CANARY"}`)
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "mcp-history", "", shared)
	servers := decodeServers(t, claimAgentMcpConfigForTest(t, runtimeID))
	if len(servers) != 1 || servers["history-shared"] == nil {
		t.Fatal("normal claim did not carry server")
	}
	var issueID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT issue_id, agent_id FROM agent_task_queue WHERE runtime_id=$1`, runtimeID).Scan(&issueID, &agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM agent_mcp_server WHERE agent_id=$1`, agentID); err != nil {
		t.Fatal(err)
	}
	req := withURLParam(newRequest("GET", "/api/issues/"+issueID+"/task-runs", nil), "id", issueID)
	w := httptest.NewRecorder()
	testHandler.ListTasksByIssue(w, req)
	if w.Code != 200 {
		t.Fatalf("ordinary run read failed: %d %s", w.Code, w.Body.String())
	}
	var runs []map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("want one run, got %d", len(runs))
	}
	var sources []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(runs[0]["managed_mcp_sources"], &sources); err != nil {
		t.Fatalf("claim source history unavailable after binding edit: %v", err)
	}
	if len(sources) != 1 || sources[0].Name != "history-shared" || sources[0].Source != "workspace" {
		t.Fatalf("winning source history = %+v", sources)
	}
}

func claimMcpHistoryForTest(t *testing.T, h *Handler, runtimeID string) AgentTaskResponse {
	t.Helper()
	req := withURLParam(newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil), "runtimeId", runtimeID)
	w := httptest.NewRecorder()
	h.ClaimTaskByRuntime(w, req)
	if w.Code != 200 {
		t.Fatalf("claim status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Task == nil || response.Task.Agent == nil {
		t.Fatal("expected normal authorized claim")
	}
	return *response.Task
}

func TestManagedMcpClaimSourcesFollowActualLayers(t *testing.T) {
	for _, tc := range []struct {
		name, own, overlay       string
		workspace, enabled, gate bool
		expected                 map[string]string
		unavailable              bool
	}{
		{name: "three layers", workspace: true, enabled: true, gate: true,
			own:      `{"mcpServers":{"shared":{"url":"https://agent.example/SECRET_CANARY"},"own":{}}}`,
			overlay:  `{"mcpServers":{"shared":{"url":"https://task.example/SECRET_CANARY"},"overlay":{}}}`,
			expected: map[string]string{"shared": "task", "own": "agent", "overlay": "task"}},
		{name: "agent wins workspace", workspace: true, enabled: true,
			own: `{"mcpServers":{"shared":{}}}`, expected: map[string]string{"shared": "agent"}},
		{name: "canonical wins legacy", workspace: true, enabled: true,
			own:      `{"mcp":{"shared":{"url":"https://legacy.example"},"legacy":{}},"mcpServers":{"shared":{"url":"https://canonical.example"}}}`,
			expected: map[string]string{"shared": "agent", "legacy": "agent"}},
		{name: "legacy wins workspace", workspace: true, enabled: true,
			own: `{"mcp":{"shared":{}}}`, expected: map[string]string{"shared": "agent"}},
		{name: "disabled binding", workspace: true, enabled: false, expected: map[string]string{}},
		{name: "unbound library", expected: map[string]string{}},
		{name: "nil own", workspace: true, enabled: true, expected: map[string]string{"shared": "workspace"}},
		{name: "empty own", own: `{}`, workspace: true, enabled: true, expected: map[string]string{"shared": "workspace"}},
		{name: "null own", own: `null`, expected: map[string]string{}},
		{name: "malformed own fallback", own: `{"mcpServers":[]}`, workspace: true, enabled: true, unavailable: true},
		{name: "malformed overlay fallback", own: `{"mcpServers":{"own":{}}}`, overlay: `{"mcpServers":{"bad":1}}`, gate: true, expected: map[string]string{"own": "agent"}},
		{name: "overlay gate disabled", own: `{"mcpServers":{"own":{}}}`, overlay: `{"mcpServers":{"overlay":{}}}`, expected: map[string]string{"own": "agent"}},
		{name: "legacy shadowed by overlay canonical", own: `{"mcp":{"legacy":{}}}`, overlay: `{"mcpServers":{"overlay":{}}}`, gate: true, expected: map[string]string{"overlay": "task"}},
		{name: "legacy overlay ignored with base", own: `{"mcpServers":{"own":{}}}`, overlay: `{"mcp":{"legacy":{}}}`, gate: true, expected: map[string]string{"own": "agent"}},
		{name: "legacy overlay alone", overlay: `{"mcp":{"legacy":{}}}`, gate: true, expected: map[string]string{"legacy": "task"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			withComposioMCPAppsFlag(t, testHandler, tc.gate)
			shared := createWorkspaceMcpServerForTest(t, "shared", `{"url":"https://workspace.example/SECRET_CANARY","args":["SECRET_CANARY"],"env":{"KEY":"SECRET_CANARY"},"headers":{"Authorization":"SECRET_CANARY"}}`)
			var bind []string
			if tc.workspace {
				bind = []string{shared}
			}
			runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "source matrix", tc.own, bind...)
			if tc.workspace && !tc.enabled {
				dbfx.Exec(t, `UPDATE agent_mcp_server SET enabled=false WHERE server_id=$1`, shared)
			}
			if tc.overlay != "" {
				dbfx.Exec(t, `UPDATE agent_task_queue SET runtime_mcp_overlay=$1 WHERE runtime_id=$2`, []byte(tc.overlay), runtimeID)
			}
			response := claimMcpHistoryForTest(t, testHandler, runtimeID)
			var stored []byte
			dbfx.QueryRow(t, `SELECT managed_mcp_snapshot FROM agent_task_queue WHERE id=$1`, response.ID).Scan(&stored)
			if strings.Contains(string(stored), "SECRET_CANARY") || strings.Contains(string(stored), "https:") {
				t.Fatal("secret config entered snapshot")
			}
			if tc.unavailable {
				if response.ManagedMcpSources != nil || len(stored) != 0 {
					t.Fatalf("ambiguous config recorded sources: %s", stored)
				}
				return
			}
			if response.ManagedMcpSources == nil {
				t.Fatal("successful claim did not capture sources")
			}
			actual := map[string]string{}
			for _, source := range *response.ManagedMcpSources {
				actual[source.Name] = source.Source
			}
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Fatalf("sources=%v want=%v", actual, tc.expected)
			}
			effective, err := managedMcpConfigSources(response.Agent.McpConfig, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(effective) != len(actual) {
				t.Fatalf("sources do not match actual configuration")
			}
			for name := range effective {
				if _, ok := actual[name]; !ok {
					t.Errorf("wire server %q absent from history", name)
				}
			}
			task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(response.ID))
			if err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(taskToResponse(task, testWorkspaceID))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), "SECRET_CANARY") || strings.Contains(string(wire), "https:") {
				t.Fatal("config leaked into ordinary run response")
			}
			if !strings.Contains(string(wire), `"managed_mcp_sources"`) {
				t.Fatal("missing history on ordinary response")
			}
		})
	}
}

func TestManagedMcpClaimWorkspaceReadFailureKeepsAgentSource(t *testing.T) {
	ctx := context.Background()
	shared := createWorkspaceMcpServerForTest(t, "shared", `{"url":"https://workspace.example"}`)
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "failed workspace read", `{"mcpServers":{"own":{}}}`, shared)
	h := *testHandler
	h.Queries = db.New(failQueryDBTX{DBTX: testPool, failOn: "ListEnabledAgentMcpServers", err: errors.New("synthetic workspace read failure")})
	response := claimMcpHistoryForTest(t, &h, runtimeID)
	if response.ManagedMcpSources == nil || !reflect.DeepEqual(*response.ManagedMcpSources, []ManagedMcpSource{{Name: "own", Source: "agent"}}) {
		t.Fatalf("read failure sources=%v", response.ManagedMcpSources)
	}
}

func TestManagedMcpClaimSnapshotGeneration(t *testing.T) {
	ctx := context.Background()
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "generation history", `{"mcpServers":{"first":{}}}`)
	first := claimMcpHistoryForTest(t, testHandler, runtimeID)
	old, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(first.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.TaskService.RequeueTaskAfterClaimFailure(ctx, old); err != nil {
		t.Fatal(err)
	}
	var cleared []byte
	dbfx.QueryRow(t, `SELECT managed_mcp_snapshot FROM agent_task_queue WHERE id=$1`, first.ID).Scan(&cleared)
	if len(cleared) != 0 {
		t.Fatal("failed claim retained provenance")
	}
	dbfx.Exec(t, `UPDATE agent SET mcp_config='{"mcpServers":{"second":{}}}' WHERE id=$1`, first.AgentID)
	second := claimMcpHistoryForTest(t, testHandler, runtimeID)
	current, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(second.ID))
	if err != nil {
		t.Fatal(err)
	}
	if second.ManagedMcpSources == nil || !reflect.DeepEqual(*second.ManagedMcpSources, []ManagedMcpSource{{Name: "second", Source: "agent"}}) {
		t.Fatal("reclaim did not capture current config")
	}
	var before int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_token WHERE task_id=$1`, first.ID).Scan(&before)
	req := newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
	_, _, _, _, failure, err := testHandler.finalizeClaimDeliveryForTest(req, &old, runtimeID, testWorkspaceID)
	if err == nil || failure != nil {
		t.Fatalf("stale delivery should fail atomically, failure=%v err=%v", failure, err)
	}
	var after int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_token WHERE task_id=$1`, first.ID).Scan(&after)
	if before != after {
		t.Fatal("stale delivery minted a token")
	}
	updated, err := testHandler.Queries.GetAgentTask(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current.ManagedMcpSnapshot, updated.ManagedMcpSnapshot) {
		t.Fatal("stale delivery replaced current snapshot")
	}
	// A new attempt cannot borrow a snapshot even if a row is restored with it.
	updated.Attempt++
	if decodeManagedMcpSnapshot(updated) != nil {
		t.Fatal("snapshot survived an attempt mismatch")
	}
}

func TestManagedMcpClaimRetryHasOwnHistory(t *testing.T) {
	ctx := context.Background()
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "retry history", `{"mcpServers":{"parent":{}}}`)
	first := claimMcpHistoryForTest(t, testHandler, runtimeID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='failed', failure_reason='agent_error.provider_network', completed_at=now(), max_attempts=3 WHERE id=$1`, first.ID)
	parent, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(first.ID))
	if err != nil {
		t.Fatal(err)
	}
	// Retry context may contain arbitrary old metadata. Only the dedicated,
	// generation-bound column is eligible for the ordinary run projection.
	parent.Context = []byte(`{"managed_mcp_sources":[{"name":"forged","source":"task"}]}`)
	dbfx.Exec(t, `UPDATE agent_task_queue SET context=$1 WHERE id=$2`, parent.Context, first.ID)
	child, err := testHandler.TaskService.MaybeRetryFailedTask(ctx, parent)
	if err != nil || child == nil {
		t.Fatalf("normal automatic retry: child=%v err=%v", child, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id=$1`, child.ID) })
	if child.Attempt != parent.Attempt+1 || len(child.ManagedMcpSnapshot) != 0 || taskToResponse(*child, testWorkspaceID).ManagedMcpSources != nil {
		t.Fatal("retry inherited source history")
	}
	dbfx.Exec(t, `UPDATE agent SET mcp_config='{}' WHERE id=$1`, first.AgentID)
	claimed := claimMcpHistoryForTest(t, testHandler, runtimeID)
	if claimed.ID != uuidToString(child.ID) || claimed.ManagedMcpSources == nil || len(*claimed.ManagedMcpSources) != 0 {
		t.Fatal("retry should capture its own empty managed set")
	}
	old, err := testHandler.Queries.GetAgentTask(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeManagedMcpSnapshot(old); got == nil || (*got)[0].Name != "parent" {
		t.Fatal("retry changed parent history")
	}
}

func TestManagedMcpHistoryStrictProjection(t *testing.T) {
	task := db.AgentTaskQueue{ID: parseUUID("00000000-0000-4000-8000-000000000001"), Attempt: 1, DispatchedAt: pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Microsecond), Valid: true}}
	sources := []ManagedMcpSource{{Name: "safe", Source: "workspace"}}
	good, err := encodeManagedMcpSnapshot(task, &sources)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		raw   []byte
		valid bool
	}{
		{"old null", nil, false}, {"valid", good, true},
		{"unknown top level", bytes.Replace(good, []byte(`"version":1`), []byte(`"version":1,"env":{"TOKEN":"SECRET_CANARY"}`), 1), false},
		{"unknown server field", bytes.Replace(good, []byte(`"name":"safe"`), []byte(`"name":"safe","url":"SECRET_CANARY"`), 1), false},
		{"unknown label", bytes.Replace(good, []byte(`"workspace"`), []byte(`"SECRET_CANARY"`), 1), false},
		{"wrong task", bytes.Replace(good, []byte(`00000000-0000-4000-8000-000000000001`), []byte(`00000000-0000-4000-8000-000000000002`), 1), false},
		{"wrong attempt", bytes.Replace(good, []byte(`"attempt":1`), []byte(`"attempt":2`), 1), false},
		{"wrong version", bytes.Replace(good, []byte(`"version":1`), []byte(`"version":2`), 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task.ManagedMcpSnapshot = tc.raw
			response := taskToResponse(task, testWorkspaceID)
			if (response.ManagedMcpSources != nil) != tc.valid {
				t.Fatalf("valid=%v got=%v", tc.valid, response.ManagedMcpSources)
			}
			wire, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), "SECRET_CANARY") {
				t.Fatal("untrusted snapshot spread through API")
			}
		})
	}
}

func TestManagedMcpHistoryPrivateRunRedaction(t *testing.T) {
	ctx := context.Background()
	t.Run("private project", func(t *testing.T) {
		f := newPrivateProjectFixture(t)
		runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "project history", `{"mcpServers":{"PRIVATE_NAME_CANARY":{}}}`)
		dbfx.Exec(t, `UPDATE agent_task_queue SET issue_id=$1 WHERE runtime_id=$2`, f.secretIssue, runtimeID)
		claim := claimMcpHistoryForTest(t, testHandler, runtimeID)
		dbfx.Exec(t, `UPDATE agent SET visibility='workspace', permission_mode='public_to' WHERE id=$1`, claim.AgentID)
		dbfx.InsertNoID(t, "agent_invocation_target", testutil.Cols{"agent_id": claim.AgentID, "target_type": "workspace", "target_id": testWorkspaceID}, "agent_id=$1", claim.AgentID)
		req := withURLParam(newRequestAs(f.outsider, "GET", "/api/agents/"+claim.AgentID+"/tasks", nil), "id", claim.AgentID)
		w := httptest.NewRecorder()
		testHandler.ListAgentTasks(w, req)
		if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE_NAME_CANARY") || strings.Contains(w.Body.String(), "managed_mcp_sources") {
			t.Fatalf("private project leaked sources: %d %s", w.Code, w.Body.String())
		}
		var tasks []AgentTaskResponse
		if err := json.Unmarshal(w.Body.Bytes(), &tasks); err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || !tasks[0].Restricted {
			t.Fatal("private project run must use restricted allowlist")
		}
	})
	t.Run("foreign chat", func(t *testing.T) {
		f := seedChatTaskPrivacy(t)
		dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at=now() WHERE id=$1`, f.chatTaskID)
		task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(f.chatTaskID))
		if err != nil {
			t.Fatal(err)
		}
		sources := []ManagedMcpSource{{Name: "PRIVATE_CHAT_CANARY", Source: "agent"}}
		snapshot, err := encodeManagedMcpSnapshot(task, &sources)
		if err != nil {
			t.Fatal(err)
		}
		dbfx.Exec(t, `UPDATE agent_task_queue SET managed_mcp_snapshot=$1 WHERE id=$2`, snapshot, f.chatTaskID)
		for _, viewer := range []string{f.ownerID, testUserID} {
			req := withURLParam(newRequestAs(viewer, "GET", "/api/agents/"+f.agentID+"/tasks", nil), "id", f.agentID)
			w := httptest.NewRecorder()
			testHandler.ListAgentTasks(w, req)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if strings.Contains(w.Body.String(), "PRIVATE_CHAT_CANARY") != (viewer == f.ownerID) {
				t.Fatalf("viewer %s received incorrect chat source visibility", viewer)
			}
		}
	})
	t.Run("private agent", func(t *testing.T) {
		agentID, _, memberID := privateAgentTestFixture(t)
		req := withURLParam(newRequestAs(memberID, "GET", "/api/agents/"+agentID+"/tasks", nil), "id", agentID)
		w := httptest.NewRecorder()
		testHandler.ListAgentTasks(w, req)
		if w.Code != 403 || strings.Contains(w.Body.String(), "managed_mcp_sources") {
			t.Fatalf("private agent run access=%d", w.Code)
		}
	})
}

func TestManagedMcpClaimFullShadowHistoryAfterEdits(t *testing.T) {
	ctx := context.Background()
	withComposioMCPAppsFlag(t, testHandler, true)
	shared := createWorkspaceMcpServerForTest(t, "shared", `{"url":"https://workspace.example/SECRET_CANARY"}`)
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "full shadow history", `{"mcpServers":{"shared":{"url":"https://agent.example/SECRET_CANARY"}}}`, shared)
	dbfx.Exec(t, `UPDATE agent_task_queue SET runtime_mcp_overlay='{"mcpServers":{"shared":{"url":"https://task.example/SECRET_CANARY"}}}' WHERE runtime_id=$1`, runtimeID)
	claimed := claimMcpHistoryForTest(t, testHandler, runtimeID)
	servers := decodeServers(t, claimed.Agent.McpConfig)
	if servers["shared"].(map[string]any)["url"] != "https://task.example/SECRET_CANARY" {
		t.Fatal("task overlay did not actually win")
	}
	dbfx.Exec(t, `UPDATE agent SET mcp_config='{}' WHERE id=$1`, claimed.AgentID)
	dbfx.Exec(t, `DELETE FROM agent_mcp_server WHERE agent_id=$1`, claimed.AgentID)
	req := withURLParam(newRequest("GET", "/api/issues/"+claimed.IssueID+"/task-runs", nil), "id", claimed.IssueID)
	w := httptest.NewRecorder()
	testHandler.ListTasksByIssue(w, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), "SECRET_CANARY") || strings.Contains(w.Body.String(), "https:") {
		t.Fatalf("ordinary history response=%d %s", w.Code, w.Body.String())
	}
	var runs []AgentTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ManagedMcpSources == nil || !reflect.DeepEqual(*runs[0].ManagedMcpSources, []ManagedMcpSource{{Name: "shared", Source: "task"}}) {
		t.Fatalf("three-layer history changed after edits")
	}
}
