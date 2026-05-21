package mentiongate

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/grouppermissions"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	mentionGatePool        *pgxpool.Pool
	mentionGateWorkspaceID pgtype.UUID
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping cerebro/mentiongate tests: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping cerebro/mentiongate tests: db unreachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	mentionGatePool = pool
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = 'cerebro-mentiongate-tests'`); err != nil {
		fmt.Printf("workspace cleanup: %v\n", err)
		os.Exit(1)
	}
	var wsID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Mention Gate Tests', 'cerebro-mentiongate-tests', 'unit-test fixture', 'MGT')
		RETURNING id
	`).Scan(&wsID); err != nil {
		fmt.Printf("workspace insert: %v\n", err)
		os.Exit(1)
	}
	mentionGateWorkspaceID = parseUUID(wsID)

	code := m.Run()

	pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	pool.Close()
	os.Exit(code)
}

func TestCanTriggerMention_DeniesWithoutAgentGrant(t *testing.T) {
	svc, commenterID, agentID, ownerID := setupMentionGateFixture(t)
	req := httptest.NewRequest("POST", "/api/issues/comment", nil)
	req.Header.Set("X-User-ID", uuidString(commenterID))

	allowed, err := svc.CanTriggerMention(context.Background(), req, uuidString(mentionGateWorkspaceID), agentID, ownerID)
	if err != nil {
		t.Fatalf("CanTriggerMention returned error: %v", err)
	}
	if allowed {
		t.Fatal("CanTriggerMention allowed a member without group access")
	}
}

func TestChannelListenGate_DeniesWithoutAgentGrant(t *testing.T) {
	svc, commenterID, agentID, ownerID := setupMentionGateFixture(t)
	gate := svc.ChannelListenGate()
	if gate == nil {
		t.Fatal("ChannelListenGate returned nil")
	}

	allowed, err := gate(context.Background(), mentionGateWorkspaceID, commenterID, agentID, ownerID)
	if err != nil {
		t.Fatalf("ChannelListenGate returned error: %v", err)
	}
	if allowed {
		t.Fatal("ChannelListenGate allowed a member without group access")
	}
}

func setupMentionGateFixture(t *testing.T) (*Service, pgtype.UUID, pgtype.UUID, pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	q := db.New(mentionGatePool)
	cq := cerebrodb.New(mentionGatePool)
	svc := New(q, grouppermissions.NewService(cq, events.New()))

	ownerID := createMentionGateMember(t, "agent-owner")
	commenterID := createMentionGateMember(t, "commenter")

	var runtimeID string
	if err := mentionGatePool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at
		) VALUES ($1, NULL, 'Mention Gate Runtime', 'cloud', 'test', 'online', 'device', '{}'::jsonb, now())
		RETURNING id
	`, mentionGateWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		mentionGatePool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	var agentID string
	if err := mentionGatePool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'mention-gate-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, mentionGateWorkspaceID, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		mentionGatePool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	return svc, commenterID, parseUUID(agentID), ownerID
}

func createMentionGateMember(t *testing.T, name string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	email := name + "@cerebro-mentiongate-tests.local"
	var userID string
	if err := mentionGatePool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, name, email).Scan(&userID); err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	if _, err := mentionGatePool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, mentionGateWorkspaceID, userID); err != nil {
		t.Fatalf("create member %q: %v", name, err)
	}
	t.Cleanup(func() {
		mentionGatePool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return parseUUID(userID)
}

func parseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}

func uuidString(u pgtype.UUID) string {
	return util.UUIDToString(u)
}
