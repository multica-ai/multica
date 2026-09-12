package wecom

// attachment_revocation_db_test.go — the attachment write path re-reads the
// installation's permission from the DATABASE, not from a mock, at each stage
// that is about to put bytes toward the chat. The unit tests beside it
// (media_upload_test.go) prove the BeforeChunk hook stops the next chunk; this
// one proves the hook is wired to the real row, so a person revoking the bot
// mid-upload — an UPDATE on channel_installation — is what stops the file.

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSendAttachment_ARevokeInTheDatabaseMidUploadStopsTheFile(t *testing.T) {
	pool := twoReplicaDB(t)
	ctx := context.Background()

	wsID, userID, agentID, instID := mustTestUUID(t), mustTestUUID(t), mustTestUUID(t), mustTestUUID(t)
	tag := uuidStringPub(instID)[:8]
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, instID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	exec(`INSERT INTO workspace (id, name, slug) VALUES ($1, $2, $3)`, wsID, "revoke "+tag, "revoke-"+tag)
	exec(`INSERT INTO "user" (id, name, email) VALUES ($1, $2, $3)`, userID, "Revoke "+tag, "revoke-"+tag+"@example.com")
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode) VALUES ($1, $2, $3, 'local')`, agentID, wsID, "revoke-agent-"+tag)
	exec(`INSERT INTO channel_installation (id, workspace_id, agent_id, channel_type, status, installer_user_id)
	      VALUES ($1, $2, $3, 'wecom', 'active', $4)`, instID, wsID, agentID, userID)

	// Five chunks at the ladder's middle step of three in flight: the fourth
	// and fifth have to wait for a slot, and that wait is where the revocation
	// lands.
	data := make([]byte, mediaChunkBytes*4+1)
	objects := &fakeObjectStore{key: "u", data: data}
	conn := newMediaConn()
	arrived, release := conn.holdChunks()
	sender := conn.newSender()
	reg := newSendersRegistry()
	reg.set(instID, sender)
	o := NewOutbound(db.New(pool), reg, slog.Default(), WithAttachments(objects))

	done := make(chan error, 1)
	go func() {
		_, err := o.sendAttachment(ctx, sender, db.Attachment{
			ID: mustTestUUID(t), Filename: "big.bin", ContentType: "application/octet-stream",
			Url: "u", SizeBytes: int64(len(data)),
		}, attachmentTarget{InstallationID: instID, ChatID: "CHAT_1", ChatType: 1})
		done <- err
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("chunk %d never reached the wire", i+1)
		}
	}

	// The person removes the bot: one row flips in the database. Nothing else
	// in the process is told.
	exec(`UPDATE channel_installation SET status = 'revoked' WHERE id = $1`, instID)
	release()

	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the delivery never returned")
	}
	if !errors.Is(err, errMediaPermissionWithdrawn) {
		t.Fatalf("delivery reported %v, want errMediaPermissionWithdrawn", err)
	}
	if n := len(conn.cmdFrames(cmdUploadMediaChunk)); n != 3 {
		t.Errorf("%d chunk frames on the wire, want 3: chunks kept going after the row said revoked", n)
	}
	if n := len(conn.cmdFrames(cmdUploadMediaFinish)); n != 0 {
		t.Errorf("%d finish frames on the wire, want 0", n)
	}
	if got := mediaSends(t, conn); len(got) != 0 {
		t.Errorf("sent %d file(s) into a chat whose owner had removed the bot", len(got))
	}
}
