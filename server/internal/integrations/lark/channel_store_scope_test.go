package lark

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// channelScopeTestDB connects to the test Postgres (DATABASE_URL or the default
// local DSN, same as the handler suite) and returns a pool, or skips when no
// migrated database is reachable. Kept local to this file so the rest of the
// lark package stays DB-free.
func channelScopeTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	var present bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.channel_installation') IS NOT NULL").Scan(&present); err != nil || !present {
		pool.Close()
		t.Skip("channel_installation not present (database not migrated)")
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestChannelStore_ScopesToFeishu is the MUL-3515 regression guard: the
// Lark/Feishu wrappers on ChannelStore must never read another channel_type's
// rows, even when a non-Feishu installation / chat-session binding / outbound
// card shares the same workspace, chat_session, or task. (Member-removal and
// chat-session cleanup deliberately stay all-channel; that is covered by the
// handler tests.)
func TestChannelStore_ScopesToFeishu(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(db.New(pool))

	// Synthetic, distinctive identifiers. channel_* has no foreign keys, so
	// these rows need no parent records and nothing cascades — which is also
	// why the test cleans up explicitly, by deterministic key, before and
	// after (a killed prior run must not leave colliding rows behind).
	const (
		feishuApp     = "cli_scope_feishu"
		slackApp      = "cli_scope_slack"
		wsID          = "5c09e000-0000-4000-8000-000000000001"
		agentID       = "5c09e000-0000-4000-8000-000000000002"
		chatSessionID = "5c09e000-0000-4000-8000-000000000003"
		taskID        = "5c09e000-0000-4000-8000-000000000004"
		installerID   = "5c09e000-0000-4000-8000-000000000005"
	)
	clean := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`,
			[]string{feishuApp, slackApp})
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM channel_outbound_card_message WHERE task_id = $1`, taskID)
	}
	clean()
	t.Cleanup(clean)

	insertInstallation := func(channelType, app string) pgtype.UUID {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id)
VALUES ($1, $2, $3, jsonb_build_object('app_id', $4::text), $5)
RETURNING id
`, wsID, agentID, channelType, app, installerID).Scan(&id); err != nil {
			t.Fatalf("insert %s installation: %v", channelType, err)
		}
		return util.MustParseUUID(id)
	}
	feishuID := insertInstallation("feishu", feishuApp)
	slackID := insertInstallation("slack", slackApp)

	// A non-Feishu binding/card sharing this test's chat_session and task.
	if _, err := pool.Exec(ctx, `
INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
VALUES ($1, $2, 'slack', 'oc_scope_slack', 'p2p')
`, chatSessionID, slackID); err != nil {
		t.Fatalf("insert slack chat binding: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO channel_outbound_card_message (chat_session_id, task_id, channel_type, channel_chat_id, channel_card_message_id, status)
VALUES ($1, $2, 'slack', 'oc_scope_slack', 'om_scope_slack', 'pending')
`, chatSessionID, taskID); err != nil {
		t.Fatalf("insert slack outbound card: %v", err)
	}

	wsUUID := util.MustParseUUID(wsID)
	sessionUUID := util.MustParseUUID(chatSessionID)
	taskUUID := util.MustParseUUID(taskID)

	// --- installation reads: Feishu visible, Slack invisible ---

	if got, err := store.GetLarkInstallation(ctx, feishuID); err != nil || got.AppID != feishuApp {
		t.Fatalf("GetLarkInstallation(feishu): got app=%q err=%v, want app=%q nil", got.AppID, err, feishuApp)
	}
	if _, err := store.GetLarkInstallation(ctx, slackID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetLarkInstallation(slack): err=%v, want pgx.ErrNoRows (scoped out)", err)
	}

	if got, err := store.GetLarkInstallationInWorkspace(ctx, GetInstallationInWorkspaceParams{ID: feishuID, WorkspaceID: wsUUID}); err != nil || got.AppID != feishuApp {
		t.Fatalf("GetLarkInstallationInWorkspace(feishu): got app=%q err=%v, want app=%q nil", got.AppID, err, feishuApp)
	}
	if _, err := store.GetLarkInstallationInWorkspace(ctx, GetInstallationInWorkspaceParams{ID: slackID, WorkspaceID: wsUUID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetLarkInstallationInWorkspace(slack): err=%v, want pgx.ErrNoRows (scoped out)", err)
	}

	// list-by-workspace: only the Feishu installation in this workspace
	byWs, err := store.ListLarkInstallationsByWorkspace(ctx, wsUUID)
	if err != nil {
		t.Fatalf("ListLarkInstallationsByWorkspace: %v", err)
	}
	if len(byWs) != 1 || byWs[0].AppID != feishuApp {
		apps := make([]string, len(byWs))
		for i, r := range byWs {
			apps[i] = r.AppID
		}
		t.Fatalf("ListLarkInstallationsByWorkspace: got apps=%v, want exactly [%s]", apps, feishuApp)
	}

	// (ListActiveLarkInstallations channel-type + live workspace/agent scoping
	// is covered by TestListActiveLarkInstallations_SkipsOrphans in the handler
	// package, which has real workspace/agent fixtures the JOIN now requires.)

	// --- outbound reads: a Slack binding/card must not be seen as Feishu ---

	if _, err := store.GetLarkChatSessionBindingBySession(ctx, sessionUUID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetLarkChatSessionBindingBySession(slack-bound session): err=%v, want pgx.ErrNoRows (scoped out)", err)
	}
	if _, err := store.GetLarkOutboundCardByTask(ctx, taskUUID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetLarkOutboundCardByTask(slack card): err=%v, want pgx.ErrNoRows (scoped out)", err)
	}
}

func TestChannelStore_UpsertLarkInstallationReclaimsDeadAppIDOwners(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(db.New(pool))

	type staleCase struct {
		name        string
		appID       string
		oldWS       string
		oldRuntime  string
		oldAgent    string
		newWS       string
		newRuntime  string
		newAgent    string
		status      string
		archived    bool
		orphanOwner bool
	}

	cases := []staleCase{
		{
			name:       "revoked",
			appID:      "issue4810-revoked",
			oldWS:      "48100000-0000-4000-8000-000000000001",
			oldRuntime: "48100000-0000-4000-8000-000000000002",
			oldAgent:   "48100000-0000-4000-8000-000000000003",
			newWS:      "48100000-0000-4000-8000-000000000004",
			newRuntime: "48100000-0000-4000-8000-000000000005",
			newAgent:   "48100000-0000-4000-8000-000000000006",
			status:     "revoked",
		},
		{
			name:       "archived agent",
			appID:      "issue4810-archived",
			oldWS:      "48100000-0000-4000-8000-000000000011",
			oldRuntime: "48100000-0000-4000-8000-000000000012",
			oldAgent:   "48100000-0000-4000-8000-000000000013",
			newWS:      "48100000-0000-4000-8000-000000000014",
			newRuntime: "48100000-0000-4000-8000-000000000015",
			newAgent:   "48100000-0000-4000-8000-000000000016",
			status:     "active",
			archived:   true,
		},
		{
			name:        "orphaned owner",
			appID:       "issue4810-orphaned",
			oldWS:       "48100000-0000-4000-8000-000000000021",
			oldRuntime:  "48100000-0000-4000-8000-000000000022",
			oldAgent:    "48100000-0000-4000-8000-000000000023",
			newWS:       "48100000-0000-4000-8000-000000000024",
			newRuntime:  "48100000-0000-4000-8000-000000000025",
			newAgent:    "48100000-0000-4000-8000-000000000026",
			status:      "active",
			orphanOwner: true,
		},
	}

	cleanApps := []string{"issue4810-revoked", "issue4810-archived", "issue4810-orphaned"}
	cleanWorkspaces := []string{
		"48100000-0000-4000-8000-000000000001",
		"48100000-0000-4000-8000-000000000004",
		"48100000-0000-4000-8000-000000000011",
		"48100000-0000-4000-8000-000000000014",
		"48100000-0000-4000-8000-000000000024",
	}
	clean := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`,
			cleanApps)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = ANY($1)`, cleanWorkspaces)
	}
	clean()
	t.Cleanup(clean)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.orphanOwner {
				seedChannelOwner(t, ctx, pool, tc.oldWS, tc.oldRuntime, tc.oldAgent, tc.archived)
			}
			seedChannelOwner(t, ctx, pool, tc.newWS, tc.newRuntime, tc.newAgent, false)

			var existingID pgtype.UUID
			if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, status, installer_user_id)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text, 'app_secret_encrypted', '', 'bot_open_id', 'old_bot'), $4, $5)
RETURNING id
`, tc.oldWS, tc.oldAgent, tc.appID, tc.status, tc.newAgent).Scan(&existingID); err != nil {
				t.Fatalf("insert existing installation: %v", err)
			}

			got, err := store.UpsertLarkInstallation(ctx, UpsertInstallationParams{
				WorkspaceID:        uuidFromString(t, tc.newWS),
				AgentID:            uuidFromString(t, tc.newAgent),
				AppID:              tc.appID,
				AppSecretEncrypted: []byte("new-secret"),
				BotOpenID:          "new_bot",
				InstallerUserID:    uuidFromString(t, tc.newAgent),
				Region:             string(RegionFeishu),
			})
			if err != nil {
				t.Fatalf("UpsertLarkInstallation: %v", err)
			}
			if got.ID != existingID {
				t.Fatalf("expected stale row to be reclaimed in place, got id %s want %s", uuidString(got.ID), uuidString(existingID))
			}
			if got.AppID != tc.appID || got.Status != string(InstallationActive) {
				t.Fatalf("got app=%q status=%q, want app=%q status=%q", got.AppID, got.Status, tc.appID, InstallationActive)
			}
			if got.WorkspaceID != uuidFromString(t, tc.newWS) || got.AgentID != uuidFromString(t, tc.newAgent) {
				t.Fatalf("got owner workspace=%s agent=%s, want workspace=%s agent=%s",
					uuidString(got.WorkspaceID), uuidString(got.AgentID), tc.newWS, tc.newAgent)
			}
		})
	}
}

func TestChannelStore_UpsertLarkInstallationRefusesLiveAppIDOwner(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(db.New(pool))

	const (
		appID      = "issue4810-live-owner"
		oldWS      = "48100000-0000-4000-8000-000000000031"
		oldRuntime = "48100000-0000-4000-8000-000000000032"
		oldAgent   = "48100000-0000-4000-8000-000000000033"
		newWS      = "48100000-0000-4000-8000-000000000034"
		newRuntime = "48100000-0000-4000-8000-000000000035"
		newAgent   = "48100000-0000-4000-8000-000000000036"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE config->>'app_id' = $1`, appID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM workspace WHERE id = ANY($1)`, []string{oldWS, newWS})
	}
	clean()
	t.Cleanup(clean)

	seedChannelOwner(t, ctx, pool, oldWS, oldRuntime, oldAgent, false)
	seedChannelOwner(t, ctx, pool, newWS, newRuntime, newAgent, false)

	var existingID pgtype.UUID
	if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text, 'app_secret_encrypted', '', 'bot_open_id', 'old_bot'), $4)
RETURNING id
`, oldWS, oldAgent, appID, oldAgent).Scan(&existingID); err != nil {
		t.Fatalf("insert existing installation: %v", err)
	}

	_, err := store.UpsertLarkInstallation(ctx, UpsertInstallationParams{
		WorkspaceID:        uuidFromString(t, newWS),
		AgentID:            uuidFromString(t, newAgent),
		AppID:              appID,
		AppSecretEncrypted: []byte("new-secret"),
		BotOpenID:          "new_bot",
		InstallerUserID:    uuidFromString(t, newAgent),
		Region:             string(RegionFeishu),
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("UpsertLarkInstallation live owner err=%v, want pgx.ErrNoRows", err)
	}

	row, err := store.GetLarkInstallation(ctx, existingID)
	if err != nil {
		t.Fatalf("GetLarkInstallation existing: %v", err)
	}
	if row.WorkspaceID != uuidFromString(t, oldWS) || row.AgentID != uuidFromString(t, oldAgent) || row.BotOpenID != "old_bot" {
		t.Fatalf("live owner row was modified: workspace=%s agent=%s bot=%q",
			uuidString(row.WorkspaceID), uuidString(row.AgentID), row.BotOpenID)
	}
}

func seedChannelOwner(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, runtimeID, agentID string, archived bool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO workspace (id, name, slug, description)
VALUES ($1, $2, $3, '')
ON CONFLICT (id) DO NOTHING
`, workspaceID, "issue4810 "+workspaceID, "issue4810-"+workspaceID); err != nil {
		t.Fatalf("insert workspace %s: %v", workspaceID, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO agent_runtime (id, workspace_id, daemon_id, name, runtime_mode, provider, status)
VALUES ($1, $2, $3, 'issue4810 runtime', 'local', 'multica_daemon', 'online')
ON CONFLICT (id) DO NOTHING
`, runtimeID, workspaceID, "issue4810-"+runtimeID); err != nil {
		t.Fatalf("insert runtime %s: %v", runtimeID, err)
	}

	archivedSQL := "NULL"
	if archived {
		archivedSQL = "now()"
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO agent (
    id, workspace_id, name, description, runtime_mode, runtime_config,
    runtime_id, visibility, max_concurrent_tasks, archived_at
)
VALUES ($1, $2, 'issue4810 agent', '', 'local', '{}'::jsonb, $3, 'workspace', 1, `+archivedSQL+`)
ON CONFLICT (id) DO NOTHING
`, agentID, workspaceID, runtimeID); err != nil {
		t.Fatalf("insert agent %s: %v", agentID, err)
	}
}
