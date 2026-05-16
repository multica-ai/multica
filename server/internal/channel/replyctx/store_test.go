package replyctx_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channel/replyctx"
)

func TestInMemoryStore_UpsertAndLookup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := replyctx.NewInMemoryStore()

	wsID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	issueID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	item := replyctx.Context{
		ConnectionID:    "feishu",
		ExternalUserID:  "ou_user1",
		WorkspaceID:     wsID,
		IssueID:         issueID,
		IssueIdentifier: "STA-1",
		IssueTitle:      "测试标题",
		ExpiresAt:       time.Now().Add(time.Hour),
	}

	if err := store.Upsert(ctx, item); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, ok, err := store.Lookup(ctx, "feishu", "ou_user1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected context to be found")
	}
	if got.IssueIdentifier != "STA-1" {
		t.Errorf("IssueIdentifier = %q, want STA-1", got.IssueIdentifier)
	}
	if got.IssueTitle != "测试标题" {
		t.Errorf("IssueTitle = %q, want 测试标题", got.IssueTitle)
	}
}

func TestInMemoryStore_LookupExpired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := replyctx.NewInMemoryStore()

	wsID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	issueID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	item := replyctx.Context{
		ConnectionID:    "feishu",
		ExternalUserID:  "ou_user1",
		WorkspaceID:     wsID,
		IssueID:         issueID,
		IssueIdentifier: "STA-1",
		ExpiresAt:       time.Now().Add(-time.Minute),
	}

	if err := store.Upsert(ctx, item); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	_, ok, err := store.Lookup(ctx, "feishu", "ou_user1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Error("expected expired context to not be found")
	}
}

func TestInMemoryStore_LookupNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := replyctx.NewInMemoryStore()

	_, ok, err := store.Lookup(ctx, "feishu", "ou_unknown", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Error("expected no context for unknown user")
	}
}

func TestInMemoryStore_Clear(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := replyctx.NewInMemoryStore()

	wsID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	issueID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	item := replyctx.Context{
		ConnectionID:    "feishu",
		ExternalUserID:  "ou_user1",
		WorkspaceID:     wsID,
		IssueID:         issueID,
		IssueIdentifier: "STA-1",
		ExpiresAt:       time.Now().Add(time.Hour),
	}

	if err := store.Upsert(ctx, item); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.Clear(ctx, "feishu", "ou_user1"); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	_, ok, err := store.Lookup(ctx, "feishu", "ou_user1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Error("expected context to be cleared")
	}
}

func TestInMemoryStore_ClearNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := replyctx.NewInMemoryStore()

	if err := store.Clear(ctx, "feishu", "ou_unknown"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
}

func TestInMemoryStore_LookupZeroNowUsesCurrentTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := replyctx.NewInMemoryStore()

	wsID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	issueID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	if err := store.Upsert(ctx, replyctx.Context{
		ConnectionID:    "feishu",
		ExternalUserID:  "ou_user1",
		WorkspaceID:     wsID,
		IssueID:         issueID,
		IssueIdentifier: "STA-1",
		ExpiresAt:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, ok, err := store.Lookup(ctx, "feishu", "ou_user1", time.Time{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected context to be found with zero now")
	}
	if got.IssueIdentifier != "STA-1" {
		t.Errorf("IssueIdentifier = %q, want STA-1", got.IssueIdentifier)
	}
}

func TestInMemoryStore_Overwrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := replyctx.NewInMemoryStore()

	wsID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	if err := store.Upsert(ctx, replyctx.Context{
		ConnectionID:    "feishu",
		ExternalUserID:  "ou_user1",
		WorkspaceID:     wsID,
		IssueID:         pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		IssueIdentifier: "STA-1",
		ExpiresAt:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := store.Upsert(ctx, replyctx.Context{
		ConnectionID:    "feishu",
		ExternalUserID:  "ou_user1",
		WorkspaceID:     wsID,
		IssueID:         pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		IssueIdentifier: "STA-2",
		ExpiresAt:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, ok, err := store.Lookup(ctx, "feishu", "ou_user1", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected context to be found")
	}
	if got.IssueIdentifier != "STA-2" {
		t.Errorf("IssueIdentifier = %q, want STA-2", got.IssueIdentifier)
	}
}

func TestInMemoryStore_UserIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := replyctx.NewInMemoryStore()

	wsID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	if err := store.Upsert(ctx, replyctx.Context{
		ConnectionID:    "feishu",
		ExternalUserID:  "ou_user1",
		WorkspaceID:     wsID,
		IssueID:         pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		IssueIdentifier: "STA-1",
		ExpiresAt:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	_, ok, err := store.Lookup(ctx, "feishu", "ou_user2", time.Now())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Error("expected no context for different user")
	}
}
