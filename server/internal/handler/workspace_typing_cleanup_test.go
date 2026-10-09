package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDeleteWorkspace_RetainsTypingCleanupUntilAcknowledged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name": "Typing cleanup", "slug": "handler-delete-typing-cleanup",
	})
	dbfx.Exec(t, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsID, testUserID)
	agentID := dbfx.Insert(t, "agent", testutil.Cols{
		"workspace_id": wsID, "name": "Typing cleanup", "owner_id": testUserID, "runtime_mode": "cloud",
	})
	sessionID := dbfx.Insert(t, "chat_session", testutil.Cols{
		"workspace_id": wsID, "agent_id": agentID, "creator_id": testUserID,
	})
	installationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": wsID, "agent_id": agentID, "channel_type": "feishu", "installer_user_id": testUserID,
	})
	dbfx.Insert(t, "channel_chat_session_binding", testutil.Cols{
		"chat_session_id": sessionID, "installation_id": installationID,
		"channel_type": "feishu", "channel_chat_id": "typing-cleanup-room", "chat_type": "group",
	})
	messageID := dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": sessionID, "role": "user", "content": "pending input", "channel_ingested": true,
	})
	const snapshot = `{"app_id":"test-app","app_secret_enc":"test-encrypted-snapshot"}`
	reactionID := dbfx.Insert(t, "channel_typing_reaction", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": wsID,
		"chat_session_id": sessionID, "chat_message_id": messageID, "installation_id": installationID,
		"channel_message_id": "remote-input", "installation_snapshot": snapshot,
		"reaction_id": "remote-reaction", "add_finished": true,
	})
	queries := db.New(testPool)
	rows, err := queries.ClaimChannelTypingReactionCleanup(ctx, parseUUID(sessionID))
	if err != nil || len(rows) != 0 {
		t.Fatalf("active input must not be claimed: rows=%d err=%v", len(rows), err)
	}

	req := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+wsID, nil), "id", wsID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	for table, id := range map[string]string{
		"workspace": wsID, "chat_session": sessionID, "channel_installation": installationID, "chat_message": messageID,
	} {
		var exists bool
		dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1)`, id).Scan(&exists)
		if exists {
			t.Fatalf("%s source survived workspace deletion", table)
		}
	}

	// A fresh worker can claim without joining to the now-deleted workspace,
	// session or installation. The external anchor and encrypted snapshot survive.
	queries = db.New(testPool)
	rows, err = queries.ClaimChannelTypingReactionCleanup(ctx, parseUUID(sessionID))
	if err != nil || len(rows) != 1 {
		t.Fatalf("deleted workspace cleanup: rows=%d err=%v", len(rows), err)
	}
	row := rows[0]
	if row.ID != parseUUID(reactionID) || row.ChannelMessageID != "remote-input" || row.ReactionID != "remote-reaction" || !row.CleanupRequired || row.CleanedAt.Valid {
		t.Fatalf("cleanup anchor was lost or prematurely acknowledged: %+v", row)
	}
	var snapshotPreserved bool
	dbfx.QueryRow(t, `SELECT installation_snapshot = $2::jsonb FROM channel_typing_reaction WHERE id = $1`, reactionID, snapshot).Scan(&snapshotPreserved)
	if !snapshotPreserved {
		t.Fatal("cleanup lost the encrypted installation snapshot")
	}
	// Pruning must retain unacknowledged cleanup even when source data is gone.
	if err := queries.PruneChannelTypingReactionCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var pending bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM channel_typing_reaction WHERE id = $1 AND cleaned_at IS NULL)`, reactionID).Scan(&pending)
	if !pending {
		t.Fatal("pruning dropped pending workspace cleanup")
	}
	if err := queries.AcknowledgeChannelTypingReactionCleanup(ctx, db.AcknowledgeChannelTypingReactionCleanupParams{
		ID: row.ID, ReactionID: row.ReactionID,
	}); err != nil {
		t.Fatal(err)
	}
	var cleaned bool
	dbfx.QueryRow(t, `SELECT cleaned_at IS NOT NULL FROM channel_typing_reaction WHERE id = $1`, reactionID).Scan(&cleaned)
	if !cleaned {
		t.Fatal("cleanup could not be acknowledged after workspace deletion")
	}
}
