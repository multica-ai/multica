package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/sessionmode"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
	"github.com/multica-ai/multica/server/internal/util"
)

type claimParitySessionModeProfiles struct {
	config sessionmode.Config
}

func (f claimParitySessionModeProfiles) Active(context.Context, pgtype.UUID, sessionmode.Mode) (sessionmode.Config, error) {
	return f.config, nil
}

func TestClaimTaskByRuntimeStoresFinalOfferedCallableIdentities(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, visibility
		)
		VALUES ($1, 'task-mandate-parity-runtime', 'local', 'claude', 'online', '', '{}'::jsonb, now(), 'private')
		RETURNING id
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create local runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'task-mandate-parity-agent', '', 'local', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create local agent: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES (
			$1, 'Task Mandate claim parity', 'in_progress', 'none', $2, 'member',
			(SELECT COALESCE(MAX(number), 82649) + 1 FROM issue WHERE workspace_id = $1), 0
		)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	var rootCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'Run the local claim parity contract', 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&rootCommentID); err != nil {
		t.Fatalf("create session root: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_session (issue_id, root_comment_id, name, mode)
		VALUES ($1, $2, 'Task Mandate parity', 'build')
	`, issueID, rootCommentID); err != nil {
		t.Fatalf("create build session: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, trigger_comment_id
		)
		VALUES ($1, $2, $3, 'queued', 0, $4)
		RETURNING id
	`, agentID, runtimeID, issueID, rootCommentID).Scan(&taskID); err != nil {
		t.Fatalf("create queued task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	const offeredCallable = "mcp__atlas-mcp__search_registry"
	previousAccess := testHandler.runtimeToolAccess
	previousBrief := testHandler.APIConnectionBrief
	previousProfiles := testHandler.SessionModeProfiles
	testHandler.runtimeToolAccess = fakeRuntimeToolAccess{}
	testHandler.APIConnectionBrief = &fakeAPIConnBrief{tools: []CerebroAPIConnectionBriefTool{{
		Connection:    "atlas-mcp",
		Name:          offeredCallable,
		Verdict:       "allow",
		MandatePrefix: "mcp__atlas-mcp__*",
	}}}
	testHandler.SessionModeProfiles = claimParitySessionModeProfiles{config: sessionmode.Config{
		Mode:           sessionmode.Build,
		Instruction:    "Build the requested change.",
		ThinkingLevel:  "high",
		AllowedTools:   []string{offeredCallable},
		ApprovalPolicy: "inherit",
	}}
	t.Cleanup(func() {
		testHandler.runtimeToolAccess = previousAccess
		testHandler.APIConnectionBrief = previousBrief
		testHandler.SessionModeProfiles = previousProfiles
	})

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "task-mandate-parity")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: status=%d body=%s", w.Code, w.Body.String())
	}

	var claimed struct {
		Task *struct {
			ID             string               `json:"id"`
			EffectiveTools []AgentTaskToolEntry `json:"effective_tools"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimed); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if claimed.Task == nil || claimed.Task.ID != taskID {
		t.Fatalf("claimed task = %+v, want %s", claimed.Task, taskID)
	}
	offered := make([]string, 0, len(claimed.Task.EffectiveTools))
	for _, tool := range claimed.Task.EffectiveTools {
		offered = append(offered, tool.Name)
	}
	if !slices.Equal(offered, []string{offeredCallable}) {
		t.Fatalf("final offered callables = %v, want [%s]", offered, offeredCallable)
	}

	taskUUID, _ := util.ParseUUID(taskID)
	workspaceUUID, _ := util.ParseUUID(testWorkspaceID)
	agentUUID, _ := util.ParseUUID(agentID)
	snapshot, err := taskmandate.NewStore(testPool).Get(ctx, taskUUID, workspaceUUID, agentUUID)
	if err != nil {
		t.Fatalf("read stored Task Mandate: %v", err)
	}
	if !slices.Equal(snapshot.AllowedTools, offered) {
		t.Fatalf("stored Task Mandate = %v, final offered callables = %v", snapshot.AllowedTools, offered)
	}
}
