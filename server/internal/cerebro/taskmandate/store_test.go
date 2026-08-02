package taskmandate

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type mandateDB struct{ row pgx.Row }

func (d mandateDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (d mandateDB) QueryRow(context.Context, string, ...any) pgx.Row { return d.row }

type mandateRow struct {
	expiresAt time.Time
	contains  bool
	err       error
}

func (r mandateRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*time.Time)) = r.expiresAt
	*(dest[1].(*bool)) = r.contains
	return nil
}

type snapshotRow struct {
	taskID, workspaceID, agentID pgtype.UUID
	allowedTools                 []byte
	issuedAt, expiresAt          time.Time
	err                          error
}

func (r snapshotRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*pgtype.UUID)) = r.taskID
	*(dest[1].(*pgtype.UUID)) = r.workspaceID
	*(dest[2].(*pgtype.UUID)) = r.agentID
	*(dest[3].(*[]byte)) = r.allowedTools
	*(dest[4].(*time.Time)) = r.issuedAt
	*(dest[5].(*time.Time)) = r.expiresAt
	return nil
}

func validUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
}

func openMandateTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("task mandate database unavailable: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("task mandate database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestIssueRejectsSecondProducerChangingTaskIdentity(t *testing.T) {
	pool := openMandateTestPool(t)
	ctx := context.Background()
	var originalWorkspaceID, changedWorkspaceID, runtimeID, originalAgentID, changedAgentID, issueID, taskID pgtype.UUID

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('Task Mandate Original', 'task-mandate-original-' || gen_random_uuid())
		RETURNING id`).Scan(&originalWorkspaceID); err != nil {
		t.Fatalf("create original workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, originalWorkspaceID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('Task Mandate Changed', 'task-mandate-changed-' || gen_random_uuid())
		RETURNING id`).Scan(&changedWorkspaceID); err != nil {
		t.Fatalf("create changed workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, changedWorkspaceID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		VALUES ($1, 'Task Mandate Runtime', 'local', 'codex')
		RETURNING id`, originalWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'Original Producer', 'local', $2)
		RETURNING id`, originalWorkspaceID, runtimeID).Scan(&originalAgentID); err != nil {
		t.Fatalf("create original agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode)
		VALUES ($1, 'Changed Producer', 'local')
		RETURNING id`, changedWorkspaceID).Scan(&changedAgentID); err != nil {
		t.Fatalf("create changed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'Task Mandate Identity', 'agent', $2)
		RETURNING id`, originalWorkspaceID, originalAgentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id)
		VALUES ($1, $2, $3)
		RETURNING id`, originalAgentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}

	store := NewStore(pool)
	if err := store.Issue(ctx, taskID, originalWorkspaceID, originalAgentID, []string{"tools:Read"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("issue original mandate: %v", err)
	}
	secondErr := store.Issue(ctx, taskID, changedWorkspaceID, changedAgentID, []string{"tools:Write"}, time.Now().Add(2*time.Hour))
	if secondErr == nil {
		overwritten, err := store.Get(ctx, taskID, changedWorkspaceID, changedAgentID)
		if err != nil {
			t.Fatalf("second producer returned nil without a readable overwrite: %v", err)
		}
		t.Fatalf(
			"second producer overwrote task mandate: workspace_id=%v agent_id=%v allowed_tools=%v; want changed identity rejected",
			overwritten.WorkspaceID, overwritten.AgentID, overwritten.AllowedTools,
		)
	}

	original, err := store.Get(ctx, taskID, originalWorkspaceID, originalAgentID)
	if err != nil {
		t.Fatalf("read original mandate after rejected overwrite: %v", err)
	}
	if len(original.AllowedTools) != 1 || original.AllowedTools[0] != "tools:Read" {
		t.Fatalf("original mandate changed after rejected overwrite: %v", original.AllowedTools)
	}
}

func TestErrorsRemainFailClosedAndDistinct(t *testing.T) {
	if errors.Is(ErrMissing, ErrExpired) || errors.Is(ErrExpired, ErrToolDeny) {
		t.Fatal("mandate denial reasons must stay distinct")
	}
}

func TestAuthorizeRejectsExpiredMandateAtCallTime(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := NewStoreDB(mandateDB{row: mandateRow{expiresAt: now.Add(-time.Second), contains: true}})
	store.now = func() time.Time { return now }
	if err := store.Authorize(context.Background(), validUUID(), validUUID(), validUUID(), "Bash"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Authorize expired mandate = %v, want ErrExpired", err)
	}
}

func TestAuthorizeCannotBypassMissingOrOutOfScopeMandate(t *testing.T) {
	tests := []struct {
		name string
		row  mandateRow
		want error
	}{
		{name: "missing", row: mandateRow{err: pgx.ErrNoRows}, want: ErrMissing},
		{name: "tool outside snapshot", row: mandateRow{expiresAt: time.Now().Add(time.Hour), contains: false}, want: ErrToolDeny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewStoreDB(mandateDB{row: tt.row})
			if err := store.Authorize(context.Background(), validUUID(), validUUID(), validUUID(), "Bash"); !errors.Is(err, tt.want) {
				t.Fatalf("Authorize = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAuthorizeAlwaysAllowsSelfCapabilityLookup(t *testing.T) {
	for _, tool := range []string{
		"get_agent_capabilities",
		"mcp__multica__get_agent_capabilities",
		"platform:get_agent_capabilities",
	} {
		t.Run(tool, func(t *testing.T) {
			if err := (*Store)(nil).Authorize(
				context.Background(),
				pgtype.UUID{},
				pgtype.UUID{},
				pgtype.UUID{},
				tool,
			); err != nil {
				t.Fatalf("Authorize(%q) = %v, want mandate-independent self lookup", tool, err)
			}
		})
	}
}

func TestGetReturnsTheExactHistoricalSnapshotAfterExpiry(t *testing.T) {
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	id := validUUID()
	store := NewStoreDB(mandateDB{row: snapshotRow{
		taskID: id, workspaceID: id, agentID: id,
		allowedTools: []byte(`["tools:Read","firtal_registry"]`),
		issuedAt:     now.Add(-2 * time.Hour), expiresAt: now.Add(-time.Hour),
	}})
	snapshot, err := store.Get(context.Background(), id, id, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(snapshot.AllowedTools) != 2 || snapshot.AllowedTools[0] != "tools:Read" {
		t.Fatalf("allowed tools = %#v, want exact stored allowlist", snapshot.AllowedTools)
	}
	if snapshot.ExpiresAt.After(now) {
		t.Fatalf("snapshot expiry = %v, expected historical expired contract", snapshot.ExpiresAt)
	}
}

func TestGetFailsClosedForMissingOrMalformedSnapshot(t *testing.T) {
	id := validUUID()
	tests := []struct {
		name string
		row  pgx.Row
	}{
		{name: "missing", row: snapshotRow{err: pgx.ErrNoRows}},
		{name: "malformed tools", row: snapshotRow{
			taskID: id, workspaceID: id, agentID: id,
			allowedTools: []byte(`{"not":"an array"}`),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewStoreDB(mandateDB{row: tt.row})
			if _, err := store.Get(context.Background(), id, id, id); err == nil {
				t.Fatal("Get succeeded, want fail-closed error")
			}
		})
	}
}

func TestMCPServerWildcardScopesOneDirectServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool string
		want string
	}{
		{"mcp__atlas-mcp__getViewerStatus", "mcp__atlas-mcp__*"},
		{"mcp__company-brain__whoami", "mcp__company-brain__*"},
		{"mcp__multica__infisical_admin__get_api_v1_projects", "mcp__multica__*"},
		{"tools:bash", ""},
		{"mcp__atlas-mcp", ""},
	}
	for _, tc := range tests {
		if got := MCPServerWildcard(tc.tool); got != tc.want {
			t.Errorf("MCPServerWildcard(%q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}
