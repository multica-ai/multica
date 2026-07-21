package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// newResolveOriginatorPool mirrors the local-postgres pattern used in
// task_claim_race_test.go: skip when the test database is unreachable
// instead of failing, so `go test ./...` stays usable in CI / clean
// developer setups that don't run Postgres.
func newResolveOriginatorPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type stubOverlayBuilder struct {
	calls     int
	lastUser  pgtype.UUID
	lastAgent db.Agent
	resp      json.RawMessage
	apps      []runtimeapps.ConnectedApp
	err       error
	respIsNil bool
}

func (s *stubOverlayBuilder) BuildTaskOverlay(_ context.Context, originatorUserID pgtype.UUID, agent db.Agent) (runtimeapps.MCPOverlayResult, error) {
	s.calls++
	s.lastUser = originatorUserID
	s.lastAgent = agent
	if s.err != nil {
		return runtimeapps.MCPOverlayResult{}, s.err
	}
	if s.respIsNil {
		return runtimeapps.MCPOverlayResult{}, nil
	}
	return runtimeapps.MCPOverlayResult{MCPOverlay: s.resp, ConnectedApps: s.apps}, nil
}

func composioMCPAppsTestFlags(enabled bool) *featureflag.Service {
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.ComposioMCPApps, featureflag.Rule{Default: enabled})
	return featureflag.NewService(provider)
}

func TestBuildRuntimeMCPOverlaySkipsBuilderWhenComposioFlagDisabled(t *testing.T) {
	builder := &stubOverlayBuilder{
		resp: json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.example/session"}}}`),
		apps: []runtimeapps.ConnectedApp{{
			Provider:    "composio",
			ServerName:  "composio",
			ToolkitSlug: "notion",
			ToolkitName: "Notion",
		}},
	}
	svc := &TaskService{
		Composio:     builder,
		FeatureFlags: composioMCPAppsTestFlags(false),
	}
	var userBytes [16]byte
	userBytes[15] = 1
	var agentBytes [16]byte
	agentBytes[15] = 2
	got := svc.buildRuntimeMCPOverlay(context.Background(), pgtype.UUID{Bytes: userBytes, Valid: true}, db.Agent{
		ID: pgtype.UUID{Bytes: agentBytes, Valid: true},
	})
	if builder.calls != 0 {
		t.Fatalf("BuildTaskOverlay calls = %d, want 0", builder.calls)
	}
	if len(got.Overlay) != 0 || len(got.ConnectedApps) != 0 {
		t.Fatalf("flag-off overlay = %+v; want empty", got)
	}
}

// seedOriginatorFanout builds the minimal fixture for an agent→agent
// fanout chain:
//
//	human U → (member-authored comment on issue I) →
//	agent A handles task T_A with originator_user_id = U →
//	agent A posts a reply comment C (author_type=agent, source_task_id=T_A) →
//	agent B picks up C as its trigger
//
// Returns: member-authored comment id (C0), agent-authored comment id (C1, with
// source_task_id=T_A), T_A's task id, and U as pgtype.UUID. T_A's
// originator_user_id is U so the fanout / quick-create branches can prove the
// inheritance.
func seedOriginatorFanout(t *testing.T, pool *pgxpool.Pool) (memberCommentID, agentCommentID, taskAID, userID, workspaceUUID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()

	var workspaceID, agentAID, agentBID, runtimeID, issueID, taskAIDStr, userIDStr, commentMemberID, commentAgentID string

	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Resolve Originator User', 'resolve-originator-fanout@multica.test')
		RETURNING id
	`).Scan(&userIDStr); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = 'resolve-originator-fanout@multica.test'`)
	})

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('resolve-orig-ws', 'resolve-orig-ws-' || gen_random_uuid())
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userIDStr); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'r', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceID, userIDStr).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'agent-A', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, runtimeID, userIDStr).Scan(&agentAID); err != nil {
		t.Fatalf("seed agent A: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'agent-B', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, runtimeID, userIDStr).Scan(&agentBID); err != nil {
		t.Fatalf("seed agent B: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'fanout-issue', 'member', $2)
		RETURNING id
	`, workspaceID, userIDStr).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	// Agent A's task carries the originator (the human U). This is the
	// row the resolver must follow back through comment.source_task_id.
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id
		)
		VALUES ($1, $2, $3, 'completed', 0, $4, $4)
		RETURNING id
	`, agentAID, runtimeID, issueID, userIDStr).Scan(&taskAIDStr); err != nil {
		t.Fatalf("seed task A: %v", err)
	}

	// Member-authored comment (no source_task_id).
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'human comment')
		RETURNING id
	`, issueID, workspaceID, userIDStr).Scan(&commentMemberID); err != nil {
		t.Fatalf("seed member comment: %v", err)
	}

	// Agent-authored comment whose source_task_id points at task A.
	// resolveOriginatorFromTriggerComment must inherit task A's originator.
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, source_task_id)
		VALUES ($1, $2, 'agent', $3, 'agent comment', $4)
		RETURNING id
	`, issueID, workspaceID, agentAID, taskAIDStr).Scan(&commentAgentID); err != nil {
		t.Fatalf("seed agent comment: %v", err)
	}

	memberCommentID = util.MustParseUUID(commentMemberID)
	agentCommentID = util.MustParseUUID(commentAgentID)
	taskAID = util.MustParseUUID(taskAIDStr)
	userID = util.MustParseUUID(userIDStr)
	workspaceUUID = util.MustParseUUID(workspaceID)
	return
}

// TestResolveOriginatorFromTriggerComment_MemberAuthored — the base case:
// a comment authored by a workspace member IS the top-of-chain. The
// originator is the comment's own author_id.
func TestResolveOriginatorFromTriggerComment_MemberAuthored(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	memberCommentID, _, _, userID, workspaceID := seedOriginatorFanout(t, pool)
	svc := &TaskService{Queries: db.New(pool)}

	got := svc.resolveOriginatorFromTriggerComment(context.Background(), workspaceID, memberCommentID)
	if !got.Valid {
		t.Fatalf("expected valid originator for member-authored comment, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s", util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorFromTriggerComment_AgentAuthoredInheritsFromParent
// — the load-bearing fanout case. Agent A finished a task it ran on
// behalf of human U; A then posts a comment that triggers agent B. The
// trigger comment's author is A (not a human), but resolving the
// originator must walk comment.source_task_id → parent task →
// parent.originator_user_id, yielding U.
func TestResolveOriginatorFromTriggerComment_AgentAuthoredInheritsFromParent(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, agentCommentID, _, userID, workspaceID := seedOriginatorFanout(t, pool)
	svc := &TaskService{Queries: db.New(pool)}

	got := svc.resolveOriginatorFromTriggerComment(context.Background(), workspaceID, agentCommentID)
	if !got.Valid {
		t.Fatalf("expected valid originator inherited from parent task, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s (parent task's originator_user_id)",
			util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestAttributionForIssueTask_SystemCommentFallsThroughToIssueProvenance covers
// the Stage-completion cascade (MUL-4302; raised by Bohan). Closing the last
// sub-issue in a Stage wakes the parent's assignee agent through a SYSTEM-authored
// child-done comment that threads no actor. That system comment carries no human,
// so attribution must NOT stop at it (which would degrade to owner_fallback, the
// agent's own owner) — it must fall through to the PARENT issue's own provenance
// and attribute to the human who caused the parent issue to exist. Here the parent
// was created by an agent on behalf of userID (agent_create origin), so the woken
// run is delegation-accountable to userID.
func TestAttributionForIssueTask_SystemCommentFallsThroughToIssueProvenance(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, _, parentTaskID, userID, _ := seedOriginatorFanout(t, pool)
	ctx := context.Background()

	// Plant a system-authored comment on the seed's issue (mirrors the child-done
	// comment). Its issue/workspace are derived from the origin task's issue.
	var issueIDStr, workspaceIDStr string
	if err := pool.QueryRow(ctx, `
		SELECT i.id::text, i.workspace_id::text
		FROM agent_task_queue t JOIN issue i ON i.id = t.issue_id
		WHERE t.id = $1
	`, util.UUIDToString(parentTaskID)).Scan(&issueIDStr, &workspaceIDStr); err != nil {
		t.Fatalf("load issue/workspace: %v", err)
	}
	var systemCommentIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'system', '00000000-0000-0000-0000-000000000000', 'child done', 'system')
		RETURNING id
	`, issueIDStr, workspaceIDStr).Scan(&systemCommentIDStr); err != nil {
		t.Fatalf("seed system comment: %v", err)
	}
	systemCommentID := util.MustParseUUID(systemCommentIDStr)

	svc := &TaskService{Queries: db.New(pool)}
	// Parent issue created by an agent on behalf of userID (agent_create origin).
	// WorkspaceID is set so the workspace-scoped trigger-comment lookup (MUL-4252)
	// finds the system comment and classifies it (author_type=system) before the
	// fall-through to issue provenance.
	issue := db.Issue{
		CreatorType: "agent",
		OriginType:  pgtype.Text{String: "agent_create", Valid: true},
		OriginID:    parentTaskID,
		WorkspaceID: util.MustParseUUID(workspaceIDStr),
	}

	got := svc.attributionForIssueTask(ctx, issue, systemCommentID, attribution.SourceDelegation, pgtype.UUID{})
	if got.Source != attribution.SourceDelegation {
		t.Fatalf("source = %q, want delegation (system comment must fall through to issue provenance, not owner_fallback)", got.Source)
	}
	if !got.UserID.Valid || got.UserID.Bytes != userID.Bytes {
		t.Errorf("accountable = %s, want %s (the human who caused the parent issue to exist)",
			util.UUIDToString(got.UserID), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorForIssueTask_MemberCreatedNoComment covers direct issue
// assignment/creation: there is no trigger comment to inspect, but the issue's
// human creator is still the run originator for Composio overlay gating.
func TestResolveOriginatorForIssueTask_MemberCreatedNoComment(t *testing.T) {
	userID := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	svc := &TaskService{}
	issue := db.Issue{CreatorType: "member", CreatorID: userID}

	got := svc.resolveOriginatorForIssueTask(context.Background(), issue, pgtype.UUID{})
	if !got.Valid {
		t.Fatalf("expected valid originator for member-created issue, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s", util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorForIssueTask_QuickCreateIssueInheritsParentTask covers
// agent-created issues that have an explicit quick-create origin stamp. The
// issue creator is the agent, but the top-of-chain human is stored on the
// parent quick-create task and must be inherited for downstream dispatch.
func TestResolveOriginatorForIssueTask_QuickCreateIssueInheritsParentTask(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, _, parentTaskID, userID, _ := seedOriginatorFanout(t, pool)
	svc := &TaskService{Queries: db.New(pool)}
	issue := db.Issue{
		CreatorType: "agent",
		OriginType:  pgtype.Text{String: "quick_create", Valid: true},
		OriginID:    parentTaskID,
	}

	got := svc.resolveOriginatorForIssueTask(context.Background(), issue, pgtype.UUID{})
	if !got.Valid {
		t.Fatalf("expected quick-create issue to inherit originator, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s", util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorForIssueTask_AgentCreateIssueInheritsParentTask covers
// the MUL-4305 fix: an agent that creates an issue through the ordinary
// `issue create` path gets origin_type='agent_create' + origin_id=<acting
// task>. The issue creator is the agent, but the top-of-chain human lives on
// that acting task and must be inherited so downstream assignment /
// squad-leader runs (and the A2A mentions they emit) keep the originator.
func TestResolveOriginatorForIssueTask_AgentCreateIssueInheritsParentTask(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, _, parentTaskID, userID, _ := seedOriginatorFanout(t, pool)
	svc := &TaskService{Queries: db.New(pool)}
	issue := db.Issue{
		CreatorType: "agent",
		OriginType:  pgtype.Text{String: "agent_create", Valid: true},
		OriginID:    parentTaskID,
	}

	got := svc.resolveOriginatorForIssueTask(context.Background(), issue, pgtype.UUID{})
	if !got.Valid {
		t.Fatalf("expected agent_create issue to inherit originator, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s", util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestOriginatorForIssueTask_MatchesResolverForAgentCreate pins the gate/enqueue
// consistency guarantee from MUL-4305: the exported OriginatorForIssueTask
// (used by the squad-leader access gate) must return the SAME human the
// unexported resolver persists on the task row. If these drift, an
// agent-created issue could be attributed correctly on the task row yet denied
// by a gate that computed a different (empty) originator.
func TestOriginatorForIssueTask_MatchesResolverForAgentCreate(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, _, parentTaskID, userID, _ := seedOriginatorFanout(t, pool)
	svc := &TaskService{Queries: db.New(pool)}
	issue := db.Issue{
		CreatorType: "agent",
		OriginType:  pgtype.Text{String: "agent_create", Valid: true},
		OriginID:    parentTaskID,
	}

	gate := svc.OriginatorForIssueTask(context.Background(), issue, pgtype.UUID{})
	write := svc.resolveOriginatorForIssueTask(context.Background(), issue, pgtype.UUID{})
	if gate.Bytes != write.Bytes || gate.Valid != write.Valid {
		t.Fatalf("gate originator %s != write originator %s",
			util.UUIDToString(gate), util.UUIDToString(write))
	}
	if !gate.Valid || gate.Bytes != userID.Bytes {
		t.Errorf("gate originator = %s, want %s", util.UUIDToString(gate), util.UUIDToString(userID))
	}
}

// TestEnqueueTaskForIssueStoresRuntimeMCPOverlayInQueuedRow guards the race
// where the daemon could poll-claim a newly inserted queued task while the old
// post-insert overlay updater was still making the outbound Composio session
// call. The overlay must be computed before INSERT and returned on the queued
// row.
func TestEnqueueTaskForIssueStoresRuntimeMCPOverlayInQueuedRow(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("runtime-overlay-insert-%d@multica.test", suffix)
	workspaceSlug := fmt.Sprintf("runtime-overlay-insert-%d", suffix)

	var userIDStr, workspaceIDStr, runtimeIDStr, agentIDStr, issueIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Runtime Overlay Insert User', $1)
		RETURNING id
	`, email).Scan(&userIDStr); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userIDStr)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('runtime overlay insert ws', $1)
		RETURNING id
	`, workspaceSlug).Scan(&workspaceIDStr); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceIDStr)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceIDStr, userIDStr); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'runtime-overlay-r', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceIDStr, userIDStr).Scan(&runtimeIDStr); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, composio_toolkit_allowlist
		)
		VALUES ($1, 'runtime-overlay-agent', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb, ARRAY['notion'])
		RETURNING id
	`, workspaceIDStr, runtimeIDStr, userIDStr).Scan(&agentIDStr); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority
		)
		VALUES ($1, 'runtime overlay issue', 'member', $2, 'agent', $3, 'medium')
		RETURNING id
	`, workspaceIDStr, userIDStr, agentIDStr).Scan(&issueIDStr); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.example/session"}}}`)
	builder := &stubOverlayBuilder{
		resp: overlay,
		apps: []runtimeapps.ConnectedApp{{
			Provider:    "composio",
			ServerName:  "composio",
			ToolkitSlug: "notion",
			ToolkitName: "Notion",
		}},
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New(), Composio: builder, FeatureFlags: composioMCPAppsTestFlags(true)}
	userID := util.MustParseUUID(userIDStr)
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueIDStr),
		AssigneeID:   util.MustParseUUID(agentIDStr),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    userID,
		WorkspaceID:  util.MustParseUUID(workspaceIDStr),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}
	if builder.calls != 1 {
		t.Fatalf("BuildTaskOverlay calls = %d, want 1", builder.calls)
	}
	if len(task.RuntimeMcpOverlay) == 0 {
		t.Fatalf("returned queued task has empty runtime_mcp_overlay")
	}
	if len(task.RuntimeConnectedApps) == 0 {
		t.Fatalf("returned queued task has empty runtime_connected_apps")
	}
	var storedOverlay []byte
	var storedApps []byte
	if err := pool.QueryRow(ctx, `SELECT runtime_mcp_overlay, runtime_connected_apps FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&storedOverlay, &storedApps); err != nil {
		t.Fatalf("read stored overlay: %v", err)
	}
	if len(storedOverlay) == 0 {
		t.Fatal("stored queued task has empty runtime_mcp_overlay")
	}
	if len(storedApps) == 0 {
		t.Fatal("stored queued task has empty runtime_connected_apps")
	}
	var apps []runtimeapps.ConnectedApp
	if err := json.Unmarshal(storedApps, &apps); err != nil {
		t.Fatalf("unmarshal stored connected apps: %v", err)
	}
	if len(apps) != 1 || apps[0].ToolkitSlug != "notion" {
		t.Fatalf("stored connected apps = %+v; want notion", apps)
	}
}

// TestResolveOriginatorFromTriggerComment_InvalidCommentID — defensive
// branch. An invalid pgtype.UUID must short-circuit before any DB query
// and return invalid.
func TestResolveOriginatorFromTriggerComment_InvalidCommentID(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	svc := &TaskService{Queries: db.New(pool)}
	got := svc.resolveOriginatorFromTriggerComment(context.Background(), pgtype.UUID{}, pgtype.UUID{})
	if got.Valid {
		t.Errorf("invalid comment id must yield invalid originator, got %s", util.UUIDToString(got))
	}
}
