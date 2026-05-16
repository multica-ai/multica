package conversationctx

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Skipf("cannot parse DATABASE_URL: %v", err)
	}
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Skipf("could not create pool: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func ensureConnection(ctx context.Context, pool *pgxpool.Pool, connID string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO channel_connection (id, provider, display_name, enabled, is_default, config, secret_config, status)
		VALUES ($1, 'feishu', 'Test', true, false, '{}', '{}', 'connected')
		ON CONFLICT (id) DO NOTHING
	`, connID)
	return err
}

// tc-store-3: JSONB roundtrip for EntityRef slice.
func TestDBStore_UpsertAndGet_RoundTrip(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()
	store := NewDBStore(pool)
	connID := fmt.Sprintf("conn-roundtrip-%d", time.Now().UnixNano())
	if err := ensureConnection(ctx, pool, connID); err != nil {
		t.Fatalf("ensure connection: %v", err)
	}

	scope := Scope{
		ConnectionID: connID,
		WorkspaceID:  "ws_1",
		ChatID:       "chat_1",
		SenderID:     "user_a",
		ThreadID:     "",
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	ents := []EntityRef{
		{Key: "STA-68", Type: EntityTypeIssue, Display: "STA-68", MentionedAt: now},
	}
	cc := ConversationContext{
		Scope:     scope,
		Entities:  ents,
		ExpiresAt: now.Add(30 * time.Minute),
	}

	if err := store.Upsert(ctx, cc); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	got, ok, err := store.Get(ctx, scope)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Fatal("expected found=true")
	}
	if len(got.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got.Entities))
	}
	if got.Entities[0].Key != "STA-68" {
		t.Fatalf("expected Key=STA-68, got %s", got.Entities[0].Key)
	}
	if got.Entities[0].Type != EntityTypeIssue {
		t.Fatalf("expected Type=issue, got %s", got.Entities[0].Type)
	}
	if !got.Entities[0].MentionedAt.Equal(now) {
		t.Fatalf(" MentionedAt mismatch: expected %v, got %v", now, got.Entities[0].MentionedAt)
	}
}

// tc-3-1: within TTL, entity returned.
func TestDBStore_Get_WithinTTL(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()
	store := NewDBStore(pool)
	connID := fmt.Sprintf("conn-ttl-%d", time.Now().UnixNano())
	if err := ensureConnection(ctx, pool, connID); err != nil {
		t.Fatalf("ensure connection: %v", err)
	}

	scope := Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "c", SenderID: "u", ThreadID: ""}
	now := time.Now().UTC()
	cc := ConversationContext{
		Scope:     scope,
		Entities:  []EntityRef{{Key: "STA-68", Type: EntityTypeIssue, MentionedAt: now}},
		ExpiresAt: now.Add(30 * time.Minute),
	}
	if err := store.Upsert(ctx, cc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, ok, err := store.Get(ctx, scope)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected found=true within TTL")
	}
	if len(got.Entities) != 1 || got.Entities[0].Key != "STA-68" {
		t.Fatalf("unexpected entities: %+v", got.Entities)
	}
}

// tc-3-3 / tc-3-4: expired record returns not found.
func TestDBStore_Get_Expired(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()
	store := NewDBStore(pool)
	connID := fmt.Sprintf("conn-exp-%d", time.Now().UnixNano())
	if err := ensureConnection(ctx, pool, connID); err != nil {
		t.Fatalf("ensure connection: %v", err)
	}

	scope := Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "c", SenderID: "u", ThreadID: ""}
	now := time.Now().UTC()
	cc := ConversationContext{
		Scope:     scope,
		Entities:  []EntityRef{{Key: "STA-68", Type: EntityTypeIssue, MentionedAt: now.Add(-2 * time.Second)}},
		ExpiresAt: now.Add(-1 * time.Second),
	}
	if err := store.Upsert(ctx, cc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	_, ok, err := store.Get(ctx, scope)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected found=false for expired record")
	}
}

// tc-3-4: DeleteExpired removes old rows.
func TestDBStore_DeleteExpired(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()
	store := NewDBStore(pool)
	connID := fmt.Sprintf("conn-del-%d", time.Now().UnixNano())
	if err := ensureConnection(ctx, pool, connID); err != nil {
		t.Fatalf("ensure connection: %v", err)
	}

	scope := Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "c", SenderID: "u", ThreadID: ""}
	now := time.Now().UTC()
	cc := ConversationContext{
		Scope:     scope,
		Entities:  []EntityRef{{Key: "STA-68", Type: EntityTypeIssue, MentionedAt: now}},
		ExpiresAt: now.Add(-1 * time.Minute),
	}
	if err := store.Upsert(ctx, cc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	n, err := store.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deleted, got %d", n)
	}

	_, ok, _ := store.Get(ctx, scope)
	if ok {
		t.Fatal("expected record to be gone after DeleteExpired")
	}
}

// tc-4-1 to tc-4-4: scope isolation across all 5 dimensions.
func TestDBStore_Get_ScopeIsolation(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()
	store := NewDBStore(pool)
	connID := fmt.Sprintf("conn-iso-%d", time.Now().UnixNano())
	if err := ensureConnection(ctx, pool, connID); err != nil {
		t.Fatalf("ensure connection: %v", err)
	}

	base := Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "chat", SenderID: "user_a", ThreadID: "thread1"}
	now := time.Now().UTC()
	cc := ConversationContext{
		Scope:     base,
		Entities:  []EntityRef{{Key: "STA-68", Type: EntityTypeIssue, MentionedAt: now}},
		ExpiresAt: now.Add(30 * time.Minute),
	}
	if err := store.Upsert(ctx, cc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	cases := []struct {
		name  string
		scope Scope
	}{
		{"different sender", Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "chat", SenderID: "user_b", ThreadID: "thread1"}},
		{"different chat", Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "chat2", SenderID: "user_a", ThreadID: "thread1"}},
		{"different thread", Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "chat", SenderID: "user_a", ThreadID: "thread2"}},
		{"different workspace", Scope{ConnectionID: connID, WorkspaceID: "ws2", ChatID: "chat", SenderID: "user_a", ThreadID: "thread1"}},
		{"different connection", Scope{ConnectionID: connID + "_other", WorkspaceID: "ws", ChatID: "chat", SenderID: "user_a", ThreadID: "thread1"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok, err := store.Get(ctx, c.scope)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if ok {
				t.Fatalf("expected not found for altered scope %s", c.name)
			}
		})
	}
}

// tc-store-2: deduplication on AppendEntities.
func TestDBStore_AppendEntities_Deduplication(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()
	store := NewDBStore(pool)
	connID := fmt.Sprintf("conn-dedup-%d", time.Now().UnixNano())
	if err := ensureConnection(ctx, pool, connID); err != nil {
		t.Fatalf("ensure connection: %v", err)
	}

	scope := Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "c", SenderID: "u", ThreadID: ""}
	now := time.Now().UTC()

	// First append
	ents1 := []EntityRef{{Key: "STA-68", Type: EntityTypeIssue, MentionedAt: now}}
	if err := store.AppendEntities(ctx, scope, ents1, 5, 30*time.Minute); err != nil {
		t.Fatalf("first AppendEntities: %v", err)
	}

	// Second append with same key but newer time
	ents2 := []EntityRef{{Key: "STA-68", Type: EntityTypeIssue, MentionedAt: now.Add(1 * time.Minute)}}
	if err := store.AppendEntities(ctx, scope, ents2, 5, 30*time.Minute); err != nil {
		t.Fatalf("second AppendEntities: %v", err)
	}

	got, ok, err := store.Get(ctx, scope)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected found=true")
	}
	if len(got.Entities) != 1 {
		t.Fatalf("expected 1 entity after dedup, got %d", len(got.Entities))
	}
}

// tc-store-1: concurrent AppendEntities must not lose entries.
func TestDBStore_AppendEntities_Concurrent(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()
	store := NewDBStore(pool)
	connID := fmt.Sprintf("conn-conc-%d", time.Now().UnixNano())
	if err := ensureConnection(ctx, pool, connID); err != nil {
		t.Fatalf("ensure connection: %v", err)
	}

	scope := Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "c", SenderID: "u", ThreadID: ""}
	now := time.Now().UTC()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ents := []EntityRef{{Key: "STA-68", Type: EntityTypeIssue, MentionedAt: now}}
		if err := store.AppendEntities(ctx, scope, ents, 5, 30*time.Minute); err != nil {
			t.Errorf("goroutine1 AppendEntities: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		ents := []EntityRef{{Key: "STA-70", Type: EntityTypeIssue, MentionedAt: now.Add(1 * time.Second)}}
		if err := store.AppendEntities(ctx, scope, ents, 5, 30*time.Minute); err != nil {
			t.Errorf("goroutine2 AppendEntities: %v", err)
		}
	}()
	wg.Wait()

	got, ok, err := store.Get(ctx, scope)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected found=true")
	}
	if len(got.Entities) != 2 {
		t.Fatalf("expected 2 entities after concurrent append, got %d", len(got.Entities))
	}
	keys := make(map[string]struct{}, len(got.Entities))
	for _, e := range got.Entities {
		keys[e.Key] = struct{}{}
	}
	if _, ok := keys["STA-68"]; !ok {
		t.Fatal("missing STA-68")
	}
	if _, ok := keys["STA-70"]; !ok {
		t.Fatal("missing STA-70")
	}
}

// tc-1-1: basic get after upsert.
func TestConversationCtx_Get_AfterUpsert(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	ctx := context.Background()
	store := NewDBStore(pool)
	connID := fmt.Sprintf("conn-basic-%d", time.Now().UnixNano())
	if err := ensureConnection(ctx, pool, connID); err != nil {
		t.Fatalf("ensure connection: %v", err)
	}

	scope := Scope{ConnectionID: connID, WorkspaceID: "ws", ChatID: "c", SenderID: "u", ThreadID: ""}
	now := time.Now().UTC()
	cc := ConversationContext{
		Scope:     scope,
		Entities:  []EntityRef{{Key: "STA-68", Type: EntityTypeIssue, MentionedAt: now}},
		ExpiresAt: now.Add(30 * time.Minute),
	}
	if err := store.Upsert(ctx, cc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, ok, err := store.Get(ctx, scope)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected found")
	}
	if len(got.Entities) != 1 || got.Entities[0].Key != "STA-68" {
		t.Fatalf("unexpected result: %+v", got.Entities)
	}
}

// Helper to compare pgtype.UUID strings.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", u.Bytes[0:4], u.Bytes[4:6], u.Bytes[6:8], u.Bytes[8:10], u.Bytes[10:16])
}

// cleanScope removes test data.
func cleanScope(ctx context.Context, pool *pgxpool.Pool, scope Scope) {
	_, _ = pool.Exec(ctx, `
		DELETE FROM channel_conversation_context
		WHERE connection_id = $1 AND workspace_id = $2 AND chat_id = $3 AND sender_external_id = $4 AND thread_id = $5
	`, scope.ConnectionID, scope.WorkspaceID, scope.ChatID, scope.SenderID, scope.ThreadID)
}

func cleanConnection(ctx context.Context, pool *pgxpool.Pool, connID string) {
	_, _ = pool.Exec(ctx, `DELETE FROM channel_connection WHERE id = $1`, connID)
}

// Test entity extraction from text.
func TestExtractEntityKeys(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"STA-68 是什么？", []string{"STA-68"}},
		{"已创建 STA-99", []string{"STA-99"}},
		{"STA-99、STA-100、STA-101 已创建", []string{"STA-99", "STA-100", "STA-101"}},
		{"好的，已经处理完毕", nil},
	}
	for _, c := range cases {
		// This tests the helper that will be used by dispatcher/runtime.
		// We will import it from the intent package or define it nearby.
		got := extractEntityKeys(c.input)
		if len(got) != len(c.want) {
			t.Fatalf("input=%q expected %d keys, got %d", c.input, len(c.want), len(got))
		}
		for i := range c.want {
			if got[i].Key != c.want[i] {
				t.Fatalf("input=%q key[%d] expected %s, got %s", c.input, i, c.want[i], got[i].Key)
			}
		}
	}
}
