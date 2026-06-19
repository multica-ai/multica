package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func autoLabelTestQueries(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, db.New(pool)
}

func createAutoLabelFixture(t *testing.T, pool *pgxpool.Pool, settings string) (workspaceID string, userID string) {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("auto-label-%d@multica.ai", suffix)
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Auto Label Test User', $1)
		RETURNING id
	`, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, settings, issue_prefix)
		VALUES ('Auto Label Tests', $1, $2::jsonb, 'ALT')
		RETURNING id
	`, fmt.Sprintf("auto-label-%d", suffix), settings).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return workspaceID, userID
}

func createAutoLabelIssue(t *testing.T, pool *pgxpool.Pool, workspaceID, creatorType, creatorID, title, description string) string {
	t.Helper()
	var issueID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, description, status, priority, creator_type, creator_id, position, number)
		VALUES ($1, $2, $3, 'todo', 'medium', $4, $5, 0,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, workspaceID, title, description, creatorType, creatorID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	return issueID
}

func createReadyAutoLabelAgent(t *testing.T, pool *pgxpool.Pool, workspaceID, ownerID string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, $2, 'Auto Label Runtime', 'local', 'codex', 'online',
		        'test runtime', '{}'::jsonb, $3, now())
		RETURNING id
	`, workspaceID, fmt.Sprintf("auto-label-daemon-%d", time.Now().UnixNano()), ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert agent_runtime: %v", err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'Auto Labeler', 'Labels newly created issues', 'local', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	return agentID
}

func TestIssueAutoLabelServiceAttachesExistingLabel(t *testing.T) {
	pool, queries := autoLabelTestQueries(t)
	workspaceID, userID := createAutoLabelFixture(t, pool, `{"auto_label_new_issues":true}`)
	label, err := queries.CreateLabel(context.Background(), db.CreateLabelParams{
		WorkspaceID: util.MustParseUUID(workspaceID),
		Name:        "Bug",
		Color:       "#ef4444",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	issueID := createAutoLabelIssue(t, pool, workspaceID, "member", userID, "Fix crash in billing worker", "The background job crashes while processing invoices.")

	svc := NewIssueAutoLabelService(queries, events.New(), nil)
	if err := svc.AutoLabelCreatedIssue(context.Background(), issueID); err != nil {
		t.Fatalf("AutoLabelCreatedIssue: %v", err)
	}

	labels, err := queries.ListLabelsByIssue(context.Background(), db.ListLabelsByIssueParams{
		IssueID:     util.MustParseUUID(issueID),
		WorkspaceID: util.MustParseUUID(workspaceID),
	})
	if err != nil {
		t.Fatalf("ListLabelsByIssue: %v", err)
	}
	if len(labels) != 1 || labels[0].ID != label.ID {
		t.Fatalf("expected existing Bug label to be attached, got %+v", labels)
	}
	all, err := queries.ListLabels(context.Background(), util.MustParseUUID(workspaceID))
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected no duplicate labels, got %d", len(all))
	}
}

func TestIssueAutoLabelServiceCreatesMissingLabel(t *testing.T) {
	pool, queries := autoLabelTestQueries(t)
	workspaceID, userID := createAutoLabelFixture(t, pool, `{"auto_label_new_issues":true}`)
	issueID := createAutoLabelIssue(t, pool, workspaceID, "member", userID, "Docker image deploy failure", "The compose workflow cannot deploy the backend image.")

	svc := NewIssueAutoLabelService(queries, events.New(), nil)
	if err := svc.AutoLabelCreatedIssue(context.Background(), issueID); err != nil {
		t.Fatalf("AutoLabelCreatedIssue: %v", err)
	}

	labels, err := queries.ListLabelsByIssue(context.Background(), db.ListLabelsByIssueParams{
		IssueID:     util.MustParseUUID(issueID),
		WorkspaceID: util.MustParseUUID(workspaceID),
	})
	if err != nil {
		t.Fatalf("ListLabelsByIssue: %v", err)
	}
	if len(labels) != 2 || labels[0].Name != "bug" || labels[1].Name != "devops" {
		t.Fatalf("expected bug and devops labels, got %+v", labels)
	}
}

func TestIssueAutoLabelServiceSkipsWhenSettingOff(t *testing.T) {
	pool, queries := autoLabelTestQueries(t)
	workspaceID, userID := createAutoLabelFixture(t, pool, `{}`)
	issueID := createAutoLabelIssue(t, pool, workspaceID, "member", userID, "Fix crash on login", "The login screen fails after submit.")

	svc := NewIssueAutoLabelService(queries, events.New(), nil)
	if err := svc.AutoLabelCreatedIssue(context.Background(), issueID); err != nil {
		t.Fatalf("AutoLabelCreatedIssue: %v", err)
	}

	labels, err := queries.ListLabelsByIssue(context.Background(), db.ListLabelsByIssueParams{
		IssueID:     util.MustParseUUID(issueID),
		WorkspaceID: util.MustParseUUID(workspaceID),
	})
	if err != nil {
		t.Fatalf("ListLabelsByIssue: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("expected no labels when setting is off, got %+v", labels)
	}
}

func TestIssueAutoLabelServiceSkipsAlreadyLabeledIssue(t *testing.T) {
	pool, queries := autoLabelTestQueries(t)
	workspaceID, userID := createAutoLabelFixture(t, pool, `{"auto_label_new_issues":true}`)
	issueID := createAutoLabelIssue(t, pool, workspaceID, "member", userID, "Fix crash on login", "The login screen fails after submit.")
	label, err := queries.CreateLabel(context.Background(), db.CreateLabelParams{
		WorkspaceID: util.MustParseUUID(workspaceID),
		Name:        "manual",
		Color:       "#64748b",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if err := queries.AttachLabelToIssue(context.Background(), db.AttachLabelToIssueParams{
		IssueID:     util.MustParseUUID(issueID),
		LabelID:     label.ID,
		WorkspaceID: util.MustParseUUID(workspaceID),
	}); err != nil {
		t.Fatalf("AttachLabelToIssue: %v", err)
	}

	svc := NewIssueAutoLabelService(queries, events.New(), nil)
	if err := svc.AutoLabelCreatedIssue(context.Background(), issueID); err != nil {
		t.Fatalf("AutoLabelCreatedIssue: %v", err)
	}

	labels, err := queries.ListLabelsByIssue(context.Background(), db.ListLabelsByIssueParams{
		IssueID:     util.MustParseUUID(issueID),
		WorkspaceID: util.MustParseUUID(workspaceID),
	})
	if err != nil {
		t.Fatalf("ListLabelsByIssue: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "manual" {
		t.Fatalf("expected only manual label, got %+v", labels)
	}
}

func TestAutoLabelEligibleCreatorType(t *testing.T) {
	if !AutoLabelEligibleCreatorType("member") {
		t.Fatal("expected member-created issues to be eligible")
	}
	if !AutoLabelEligibleCreatorType("agent") {
		t.Fatal("expected agent-created issues to be eligible")
	}
	if AutoLabelEligibleCreatorType("system") {
		t.Fatal("expected unsupported creator types to be ineligible")
	}
}

func TestIssueAutoLabelServiceLabelsAgentCreatedIssueInFallbackMode(t *testing.T) {
	pool, queries := autoLabelTestQueries(t)
	workspaceID, userID := createAutoLabelFixture(t, pool, `{"auto_label_new_issues":true}`)
	issueID := createAutoLabelIssue(t, pool, workspaceID, "agent", userID, "Fix crash on login", "The login screen fails after submit.")

	svc := NewIssueAutoLabelService(queries, events.New(), nil)
	if err := svc.AutoLabelCreatedIssue(context.Background(), issueID); err != nil {
		t.Fatalf("AutoLabelCreatedIssue: %v", err)
	}

	labels, err := queries.ListLabelsByIssue(context.Background(), db.ListLabelsByIssueParams{
		IssueID:     util.MustParseUUID(issueID),
		WorkspaceID: util.MustParseUUID(workspaceID),
	})
	if err != nil {
		t.Fatalf("ListLabelsByIssue: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "bug" {
		t.Fatalf("expected bug label for agent-created issue, got %+v", labels)
	}
}

func TestIssueAutoLabelServiceEnqueuesAgentTaskWhenTaskServiceProvided(t *testing.T) {
	pool, queries := autoLabelTestQueries(t)
	workspaceID, userID := createAutoLabelFixture(t, pool, `{"auto_label_new_issues":true}`)
	agentID := createReadyAutoLabelAgent(t, pool, workspaceID, userID)
	issueID := createAutoLabelIssue(t, pool, workspaceID, "member", userID, "코드베이스 mcp 조사하고 연결하기", "MCP 연결 방식을 조사합니다.")

	taskSvc := NewTaskService(queries, pool, nil, events.New())
	svc := NewIssueAutoLabelService(queries, events.New(), nil)
	svc.TaskService = taskSvc
	if err := svc.AutoLabelCreatedIssue(context.Background(), issueID); err != nil {
		t.Fatalf("AutoLabelCreatedIssue: %v", err)
	}
	if err := svc.AutoLabelCreatedIssue(context.Background(), issueID); err != nil {
		t.Fatalf("AutoLabelCreatedIssue second call: %v", err)
	}

	tasks, err := queries.ListTasksByIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one auto-label task after dedupe, got %d", len(tasks))
	}
	if tasks[0].AgentID != util.MustParseUUID(agentID) {
		t.Fatalf("task agent = %s, want %s", util.UUIDToString(tasks[0].AgentID), agentID)
	}
	if !IsIssueAutoLabelTaskContext(tasks[0].Context) {
		t.Fatalf("expected issue_auto_label task context, got %s", string(tasks[0].Context))
	}
	if !tasks[0].ForceFreshSession {
		t.Fatal("expected auto-label task to force a fresh session")
	}

	labels, err := queries.ListLabelsByIssue(context.Background(), db.ListLabelsByIssueParams{
		IssueID:     util.MustParseUUID(issueID),
		WorkspaceID: util.MustParseUUID(workspaceID),
	})
	if err != nil {
		t.Fatalf("ListLabelsByIssue: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("expected agent task path not to attach labels synchronously, got %+v", labels)
	}
}
