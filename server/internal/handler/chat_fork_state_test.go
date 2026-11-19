package handler

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestChatForkReservationAndPublication(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := t.Context()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
	sourceID := parseUUID(dbfx.ChatSession(t, agentID, testutil.Cols{"runtime_id": runtimeID, "session_id": "source-session"}))
	destinationID := parseUUID("019fe3de-56a2-7792-8913-ee53664dfa0e")
	taskID := parseUUID("019fe3de-56a2-7792-8913-ee53664dfa0f")
	tx, err := dbfx.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)
	if _, err := q.LockChatSessionForEnqueue(ctx, sourceID); err != nil {
		t.Fatal(err)
	}

	reserve := db.ReserveChatForkSnapshotParams{SourceChatID: sourceID, TaskID: taskID,
		WorkspaceID: parseUUID(dbfx.WorkspaceID), CreatorID: parseUUID(dbfx.UserID)}
	source, err := q.ReserveChatForkSnapshot(ctx, reserve)
	if err != nil || source.ForkSnapshotTaskID != taskID || source.SessionID.String != "source-session" {
		t.Fatalf("reserve source=%+v error=%v", source, err)
	}
	if _, err := q.ReserveChatForkSnapshot(ctx, reserve); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate reservation returned %v", err)
	}
	child, err := q.CreatePendingChatFork(ctx, db.CreatePendingChatForkParams{SourceChatID: sourceID, DestinationChatID: destinationID, TaskID: taskID})
	if err != nil || child.SessionID.Valid || child.ForkedFromChatID != sourceID {
		t.Fatalf("pending child=%+v error=%v", child, err)
	}
	public := db.GetPublicChatSessionInWorkspaceParams{ID: destinationID, WorkspaceID: reserve.WorkspaceID}
	if _, err := q.GetPublicChatSessionInWorkspace(ctx, public); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pending destination was exposed: %v", err)
	}
	if _, err := q.GetPendingChatForkSource(ctx, db.GetPendingChatForkSourceParams{DestinationChatID: destinationID, TaskID: taskID}); err != nil {
		t.Fatal(err)
	}
	ready, err := q.CompletePendingChatFork(ctx, db.CompletePendingChatForkParams{DestinationChatID: destinationID, TaskID: taskID,
		SessionID: pgtype.Text{String: "child-session", Valid: true}})
	if err != nil || ready.ForkInitializationTaskID.Valid || ready.SessionID.String != "child-session" {
		t.Fatalf("published chat=%+v error=%v", ready, err)
	}
	if _, err := q.GetPublicChatSessionInWorkspace(ctx, public); err != nil {
		t.Fatal(err)
	}
	if count, err := q.ReleaseChatForkSnapshot(ctx, db.ReleaseChatForkSnapshotParams{SourceChatID: sourceID, TaskID: taskID}); err != nil || count != 1 {
		t.Fatalf("release count=%d error=%v", count, err)
	}
	original, err := q.GetChatSession(ctx, sourceID)
	if err != nil || original.ForkSnapshotTaskID.Valid || original.SessionID.String != "source-session" {
		t.Fatalf("original source=%+v error=%v", original, err)
	}
}
