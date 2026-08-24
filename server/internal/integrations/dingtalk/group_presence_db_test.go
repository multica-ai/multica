package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDingTalkGroupPresencePreservesMetadataAndAggregatesBotsDB(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	const (
		workspaceID = "d1471000-0000-4000-8000-000000000001"
		agentOneID  = "d1471000-0000-4000-8000-000000000002"
		agentTwoID  = "d1471000-0000-4000-8000-000000000003"
		installerID = "d1471000-0000-4000-8000-000000000004"
		installOne  = "d1471000-0000-4000-8000-000000000011"
		installTwo  = "d1471000-0000-4000-8000-000000000012"
		sessionOne  = "d1471000-0000-4000-8000-000000000021"
		sessionTwo  = "d1471000-0000-4000-8000-000000000022"
	)
	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_group_presence WHERE installation_id = ANY($1::uuid[])`, []string{installOne, installTwo})
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_bot_identity WHERE installation_id = ANY($1::uuid[])`, []string{installOne, installTwo})
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE installation_id = ANY($1::uuid[])`, []string{installOne, installTwo})
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE workspace_id = $1 AND channel_type = 'dingtalk'`, workspaceID)
	}
	clean()
	t.Cleanup(clean)
	for _, installation := range []struct {
		id      string
		agentID string
		appID   string
	}{
		{id: installOne, agentID: agentOneID, appID: "group-presence-one"},
		{id: installTwo, agentID: agentTwoID, appID: "group-presence-two"},
	} {
		if _, err := pool.Exec(ctx, `
INSERT INTO channel_installation (id, workspace_id, agent_id, channel_type, config, installer_user_id)
VALUES ($1, $2, $3, 'dingtalk', jsonb_build_object('app_id', $4::text), $5)
`, installation.id, workspaceID, installation.agentID, installation.appID, installerID); err != nil {
			t.Fatalf("seed installation %s: %v", installation.id, err)
		}
	}

	queries := db.New(pool)
	record := func(installationID, conversationID, title, name, issue string) {
		t.Helper()
		if _, err := queries.UpsertDingTalkBotIdentity(ctx, db.UpsertDingTalkBotIdentityParams{
			BotName: name, BotIdentityIssue: issue,
			InstallationID: util.MustParseUUID(installationID),
			WorkspaceID:    util.MustParseUUID(workspaceID),
		}); err != nil {
			t.Fatalf("record bot identity: %v", err)
		}
		if _, err := queries.RecordDingTalkGroupPresence(ctx, db.RecordDingTalkGroupPresenceParams{
			ConversationID: conversationID, ConversationTitle: title,
			InstallationID: util.MustParseUUID(installationID),
			WorkspaceID:    util.MustParseUUID(workspaceID),
		}); err != nil {
			t.Fatalf("record group presence: %v", err)
		}
	}
	recordActivity := func(installationID, conversationID string) {
		t.Helper()
		if _, err := queries.RecordDingTalkGroupActivity(ctx, db.RecordDingTalkGroupActivityParams{
			InstallationID: util.MustParseUUID(installationID),
			ConversationID: conversationID,
		}); err != nil {
			t.Fatalf("record group activity: %v", err)
		}
	}
	record(installOne, "cid-shared", "Platform", "Release Bot", "")
	record(installTwo, "cid-shared", "Platform", "", botIdentityIssueMissingChatManage)
	// A transient lookup in another group must not erase the application-level
	// permission problem observed on this installation.
	record(installTwo, "cid-repair", "Repair", "", "")
	for _, binding := range []struct {
		sessionID      string
		installationID string
	}{
		{sessionID: sessionOne, installationID: installOne},
		{sessionID: sessionTwo, installationID: installTwo},
	} {
		if _, err := pool.Exec(ctx, `
INSERT INTO channel_chat_session_binding (
  chat_session_id, installation_id, channel_type, channel_chat_id, chat_type
) VALUES ($1, $2, 'dingtalk', 'cid-shared', 'group')
`, binding.sessionID, binding.installationID); err != nil {
			t.Fatalf("seed group binding: %v", err)
		}
	}
	recordActivity(installTwo, "cid-repair")
	recordActivity(installOne, "cid-shared")
	recordActivity(installOne, "cid-shared")
	recordActivity(installTwo, "cid-shared")
	listParams := db.ListDingTalkGroupPresencesByWorkspaceParams{
		WorkspaceID: util.MustParseUUID(workspaceID),
		ActiveSince: pgtype.Timestamptz{Time: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}
	rows, err := queries.ListDingTalkGroupPresencesByWorkspace(ctx, listParams)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("presence rows = %d, want 3: %+v", len(rows), rows)
	}
	for _, row := range rows {
		wantIssue := ""
		if row.InstallationID == util.MustParseUUID(installTwo) {
			wantIssue = botIdentityIssueMissingChatManage
		}
		if row.BotIdentityIssue != wantIssue {
			t.Fatalf("transient lookup changed permission state: %+v", rows)
		}
	}

	// Empty refresh values preserve known metadata. A successful lookup in any
	// group proves the app permission is repaired and clears every stale issue.
	record(installOne, "cid-shared", "", "", "")
	record(installTwo, "cid-repair", "", "Support Bot", "")

	// A repeated observation must not make the installation look reconfigured.
	// Use a sentinel timestamp so the assertion does not depend on clock timing.
	sentinelUpdatedAt := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	if _, err := pool.Exec(ctx, `UPDATE channel_installation SET updated_at = $1 WHERE id = $2`, sentinelUpdatedAt, installOne); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE dingtalk_bot_identity SET updated_at = $1 WHERE installation_id = $2`, sentinelUpdatedAt, installOne); err != nil {
		t.Fatal(err)
	}
	record(installOne, "cid-shared", "Platform", "Release Bot", "")
	var updatedAt, identityUpdatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM channel_installation WHERE id = $1`, installOne).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM dingtalk_bot_identity WHERE installation_id = $1`, installOne).Scan(&identityUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if !updatedAt.Equal(sentinelUpdatedAt) || !identityUpdatedAt.Equal(sentinelUpdatedAt) {
		t.Fatalf("no-op observation updated timestamps to installation %s identity %s, want %s", updatedAt, identityUpdatedAt, sentinelUpdatedAt)
	}

	rows, err = queries.ListDingTalkGroupPresencesByWorkspace(ctx, listParams)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("group presence rows = %d, want two bots in one group plus repair group: %+v", len(rows), rows)
	}
	byKey := make(map[string]db.ListDingTalkGroupPresencesByWorkspaceRow, len(rows))
	for _, row := range rows {
		byKey[util.UUIDToString(row.InstallationID)+"/"+row.ConversationID] = row
	}
	first := byKey[installOne+"/cid-shared"]
	if first.ConversationTitle != "Platform" || first.BotName != "Release Bot" ||
		first.BotIdentityIssue != "" || first.MentionCount != 2 || !first.LastActiveAt.Valid {
		t.Fatalf("first bot presence = %+v", first)
	}
	second := byKey[installTwo+"/cid-shared"]
	if second.ConversationTitle != "Platform" || second.BotName != "Support Bot" ||
		second.BotIdentityIssue != "" || second.MentionCount != 1 || !second.LastActiveAt.Valid {
		t.Fatalf("second bot presence = %+v", second)
	}
	repaired := byKey[installTwo+"/cid-repair"]
	if repaired.ConversationTitle != "Repair" || repaired.BotName != "Support Bot" ||
		repaired.BotIdentityIssue != "" || repaired.MentionCount != 1 {
		t.Fatalf("repaired bot presence = %+v", repaired)
	}
	filteredRows, err := queries.ListDingTalkGroupPresencesByWorkspace(ctx, db.ListDingTalkGroupPresencesByWorkspaceParams{
		WorkspaceID:   util.MustParseUUID(workspaceID),
		FilterByAgent: true,
		AgentID:       util.MustParseUUID(agentTwoID),
		ActiveSince:   listParams.ActiveSince,
	})
	if err != nil || len(filteredRows) != 2 {
		t.Fatalf("Agent-filtered rows = %+v err %v", filteredRows, err)
	}
	for _, row := range filteredRows {
		if row.AgentID != util.MustParseUUID(agentTwoID) {
			t.Fatalf("Agent filter leaked row %+v", row)
		}
	}
	if _, err := queries.RecordDingTalkGroupActivity(ctx, db.RecordDingTalkGroupActivityParams{
		InstallationID: util.MustParseUUID(installOne),
		ConversationID: "cid-unknown",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unknown group activity error = %v, want no rows", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE channel_installation SET status = 'revoked' WHERE id = $1`, installTwo); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.RecordDingTalkGroupPresence(ctx, db.RecordDingTalkGroupPresenceParams{
		ConversationID: "cid-new", ConversationTitle: "Must not land",
		InstallationID: util.MustParseUUID(installTwo),
		WorkspaceID:    util.MustParseUUID(workspaceID),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("revoked observation error = %v, want no rows", err)
	}
	rows, err = queries.ListDingTalkGroupPresencesByWorkspace(ctx, listParams)
	if err != nil || len(rows) != 1 || rows[0].InstallationID != util.MustParseUUID(installOne) {
		t.Fatalf("revoked bot filtering = rows %+v err %v", rows, err)
	}
}

func TestDingTalkGroupPresenceConcurrentGroupsDoNotLoseUpdatesDB(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	const (
		workspaceID    = "d1472000-0000-4000-8000-000000000001"
		agentID        = "d1472000-0000-4000-8000-000000000002"
		installerID    = "d1472000-0000-4000-8000-000000000003"
		installationID = "d1472000-0000-4000-8000-000000000004"
	)
	_, _ = pool.Exec(ctx, `DELETE FROM dingtalk_group_presence WHERE installation_id = $1`, installationID)
	_, _ = pool.Exec(ctx, `DELETE FROM dingtalk_bot_identity WHERE installation_id = $1`, installationID)
	_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, installationID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_group_presence WHERE installation_id = $1`, installationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_bot_identity WHERE installation_id = $1`, installationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})
	if _, err := pool.Exec(ctx, `
INSERT INTO channel_installation (id, workspace_id, agent_id, channel_type, config, installer_user_id)
VALUES ($1, $2, $3, 'dingtalk', '{"app_id":"group-presence-concurrent"}', $4)
`, installationID, workspaceID, agentID, installerID); err != nil {
		t.Fatal(err)
	}

	queries := db.New(pool)
	const groupCount = 8
	start := make(chan struct{})
	errs := make(chan error, groupCount)
	var ready sync.WaitGroup
	ready.Add(groupCount)
	for i := range groupCount {
		go func() {
			ready.Done()
			<-start
			conversationID := fmt.Sprintf("cid-%d", i)
			_, err := queries.UpsertDingTalkBotIdentity(ctx, db.UpsertDingTalkBotIdentityParams{
				BotName:        "Concurrent Bot",
				InstallationID: util.MustParseUUID(installationID),
				WorkspaceID:    util.MustParseUUID(workspaceID),
			})
			if err == nil {
				_, err = queries.RecordDingTalkGroupPresence(ctx, db.RecordDingTalkGroupPresenceParams{
					ConversationID:    conversationID,
					ConversationTitle: fmt.Sprintf("Group %d", i),
					InstallationID:    util.MustParseUUID(installationID),
					WorkspaceID:       util.MustParseUUID(workspaceID),
				})
			}
			if err == nil {
				_, err = queries.RecordDingTalkGroupActivity(ctx, db.RecordDingTalkGroupActivityParams{
					InstallationID: util.MustParseUUID(installationID),
					ConversationID: conversationID,
				})
			}
			errs <- err
		}()
	}
	ready.Wait()
	close(start)
	for range groupCount {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent record: %v", err)
		}
	}
	rows, err := queries.ListDingTalkGroupPresencesByWorkspace(ctx, db.ListDingTalkGroupPresencesByWorkspaceParams{
		WorkspaceID: util.MustParseUUID(workspaceID),
		ActiveSince: pgtype.Timestamptz{Time: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != groupCount {
		t.Fatalf("concurrent group rows = %d, want %d: %+v", len(rows), groupCount, rows)
	}
}
