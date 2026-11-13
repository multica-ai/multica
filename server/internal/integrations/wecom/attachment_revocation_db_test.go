package wecom

// attachment_revocation_db_test.go — the attachment worker re-reads the
// installation's permission from the DATABASE, not from a double, at each frame
// it writes. The tests beside it (attachment_revocation_test.go) prove the
// ordering with a double that can change its answer; this one proves the guard
// is wired to the real row, so a person revoking the bot mid-upload — an UPDATE
// on channel_installation — is what stops the file and silences the notice.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// attachmentsFromMemory is the real *db.Queries with the one lookup that would
// need a chat_message and an attachment row answered from memory. The
// permission read — the thing under test — still goes to the database.
type attachmentsFromMemory struct {
	*db.Queries
	rows []db.Attachment
}

func (a attachmentsFromMemory) ListAttachmentsByChatMessage(context.Context, db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error) {
	return a.rows, nil
}

func TestSendAttachments_ARevokeInTheDatabaseMidUploadStopsTheFileAndTheNotice(t *testing.T) {
	pool := twoReplicaDB(t)

	// Fixtures delete in reverse creation order, so no teardown chain to keep
	// in step (AGENTS.md Testing rules).
	f := dbfx.New(pool, "", "")
	tag := uuidStringPub(mustTestUUID(t))[:8]
	f.WorkspaceID = f.Workspace(t, "revoke "+tag, "revoke-"+tag)
	f.UserID = f.User(t, "Revoke "+tag, "revoke-"+tag+"@example.com")
	agentID := f.Agent(t, "revoke-agent-"+tag, "", dbfx.Cols{"runtime_mode": "local"})
	instID := f.Insert(t, "channel_installation", dbfx.Cols{
		"workspace_id":      f.WorkspaceID,
		"agent_id":          agentID,
		"channel_type":      "wecom",
		"status":            "active",
		"installer_user_id": f.UserID,
	})
	inst, err := util.ParseUUID(instID)
	if err != nil {
		t.Fatalf("parse installation id: %v", err)
	}

	// Five chunks at the ladder's middle step of three in flight: the fourth
	// and fifth have to wait for a slot, and that wait is where the revocation
	// lands.
	data := fiveChunks()
	conn := newMediaConn()
	arrived, release := conn.holdChunks()
	sender := conn.newSender()
	reg := newSendersRegistry()
	reg.set(inst, sender)
	q := attachmentsFromMemory{Queries: db.New(pool), rows: []db.Attachment{{
		ID: mustTestUUID(t), Filename: "big.bin", ContentType: "application/octet-stream",
		Url: "u", SizeBytes: int64(len(data)),
	}}}
	o := NewOutbound(q, reg, nil, slog.Default(), WithAttachments(&fakeObjectStore{key: "u", data: data}))

	done := make(chan struct{})
	go func() {
		defer close(done)
		o.sendAttachments(context.Background(), mustTestUUID(t), mustTestUUID(t),
			attachmentTarget{InstallationID: inst, ChatID: "CHAT_1", ChatType: chatTypeSingleInt}, true)
	}()
	awaitChunks(t, arrived, 3)

	// The person removes the bot: one row flips in the database. Nothing else
	// in the process is told.
	f.Exec(t, `UPDATE channel_installation SET status = 'revoked' WHERE id = $1`, instID)
	release()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the attachment worker never returned")
	}
	if n := len(conn.cmdFrames(cmdUploadMediaChunk)); n != 3 {
		t.Errorf("%d chunk frames on the wire, want 3: chunks kept going after the row said revoked", n)
	}
	if n := len(conn.cmdFrames(cmdUploadMediaFinish)); n != 0 {
		t.Errorf("%d finish frames on the wire, want 0", n)
	}
	// Not the file, and not the "a file could not be sent" notice either.
	if n := len(conn.cmdFrames(cmdSendMsg)); n != 0 {
		t.Errorf("%d messages sent into a chat whose owner had removed the bot, want 0", n)
	}
}
