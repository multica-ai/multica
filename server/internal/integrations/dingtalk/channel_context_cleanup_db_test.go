package dingtalk

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type dingtalkContextCleanupOwner struct {
	creatorID      string
	workspaceID    string
	runtimeID      string
	agentID        string
	installationID string
	chatSessionID  string
}

func seedDingTalkContextCleanupOwner(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	kind string,
) dingtalkContextCleanupOwner {
	t.Helper()
	owner := dingtalkContextCleanupOwner{
		creatorID:      uuid.NewString(),
		workspaceID:    uuid.NewString(),
		runtimeID:      uuid.NewString(),
		agentID:        uuid.NewString(),
		installationID: uuid.NewString(),
		chatSessionID:  uuid.NewString(),
	}
	slug := "dingtalk-context-cleanup-" + uuid.NewString()
	systemKey := ""
	if kind == "system" {
		systemKey = "dingtalk_context_cleanup_" + uuid.NewString()
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed DingTalk context cleanup fixture: %v", err)
		}
	}
	exec(`INSERT INTO "user" (id, name, email) VALUES ($1, 'DingTalk context cleanup creator', $2)`, owner.creatorID, slug+"@multica.test")
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'DingTalk context cleanup', $2, '')`, owner.workspaceID, slug)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider) VALUES ($1, $2, 'DingTalk cleanup runtime', 'local', 'multica_daemon')`, owner.runtimeID, owner.workspaceID)
	exec(`
		INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id, kind, system_key)
		VALUES ($1, $2, 'DingTalk cleanup agent', 'local', $3, $4, NULLIF($5, ''))
	`, owner.agentID, owner.workspaceID, owner.runtimeID, kind, systemKey)
	exec(`
		INSERT INTO channel_installation (
			id, workspace_id, agent_id, channel_type, config, installer_user_id, status
		) VALUES ($1, $2, $3, 'dingtalk', '{}'::jsonb, $4, 'active')
	`, owner.installationID, owner.workspaceID, owner.agentID, owner.creatorID)
	exec(`
		INSERT INTO chat_session (id, workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, $4, 'retired channel chat')
	`, owner.chatSessionID, owner.workspaceID, owner.agentID, owner.creatorID)
	exec(`INSERT INTO channel_chat_context_generation (chat_session_id, revision) VALUES ($1, 1)`, owner.chatSessionID)

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_chat_context_generation WHERE chat_session_id = $1`, owner.chatSessionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, owner.chatSessionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM chat_session WHERE id = $1`, owner.chatSessionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_installation WHERE id = $1`, owner.installationID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM agent WHERE id = $1`, owner.agentID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE id = $1`, owner.runtimeID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, owner.workspaceID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, owner.creatorID)
	})
	return owner
}

func assertDingTalkContextCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner dingtalkContextCleanupOwner, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_chat_context_generation WHERE chat_session_id = $1`, owner.chatSessionID).Scan(&got); err != nil {
		t.Fatalf("count channel contexts: %v", err)
	}
	if got != want {
		t.Fatalf("channel contexts for %s = %d, want %d", owner.chatSessionID, got, want)
	}
}

func TestDeleteChannelInstallationsBySystemRuntimeAgentsCleansRetiredChatContexts(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	systemOwner := seedDingTalkContextCleanupOwner(t, ctx, pool, "system")
	userOwner := seedDingTalkContextCleanupOwner(t, ctx, pool, "user")

	if err := db.New(pool).DeleteChannelInstallationsBySystemRuntimeAgents(ctx, util.MustParseUUID(systemOwner.runtimeID)); err != nil {
		t.Fatalf("DeleteChannelInstallationsBySystemRuntimeAgents: %v", err)
	}

	assertDingTalkContextCount(t, ctx, pool, systemOwner, 0)
	assertDingTalkContextCount(t, ctx, pool, userOwner, 1)
}

func TestDeleteWorkspaceCleansRetiredChatContexts(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	target := seedDingTalkContextCleanupOwner(t, ctx, pool, "user")
	unrelated := seedDingTalkContextCleanupOwner(t, ctx, pool, "user")

	if err := db.New(pool).DeleteWorkspace(ctx, util.MustParseUUID(target.workspaceID)); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	assertDingTalkContextCount(t, ctx, pool, target, 0)
	assertDingTalkContextCount(t, ctx, pool, unrelated, 1)
}
