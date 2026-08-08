package handler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLockIdlePlatformExtensionRuntimeFiltersProviderStatusAndLiveness(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name          string
		provider      string
		status        string
		lastSeen      time.Time
		redisLiveness bool
		wantRuntime   bool
	}{
		{
			name:          "eligible redis-alive runtime",
			provider:      "platform-agent-cli",
			status:        "online",
			lastSeen:      time.Now().Add(-10 * time.Minute),
			redisLiveness: true,
			wantRuntime:   true,
		},
		{
			name:          "wrong provider",
			provider:      "codex",
			status:        "online",
			lastSeen:      time.Now(),
			redisLiveness: true,
			wantRuntime:   false,
		},
		{
			name:          "offline",
			provider:      "platform-agent-cli",
			status:        "offline",
			lastSeen:      time.Now(),
			redisLiveness: true,
			wantRuntime:   false,
		},
		{
			name:          "fresh database fallback",
			provider:      "platform-agent-cli",
			status:        "online",
			lastSeen:      time.Now().Add(-149 * time.Second),
			redisLiveness: false,
			wantRuntime:   true,
		},
		{
			name:          "stale database fallback",
			provider:      "platform-agent-cli",
			status:        "online",
			lastSeen:      time.Now().Add(-151 * time.Second),
			redisLiveness: false,
			wantRuntime:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
			runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
				Provider:   tt.provider,
				Status:     tt.status,
				LastSeenAt: tt.lastSeen,
				Visibility: "public",
				OwnerID:    testUserID,
			})

			tx, err := testPool.Begin(context.Background())
			if err != nil {
				t.Fatalf("begin allocation transaction: %v", err)
			}
			defer tx.Rollback(context.Background())

			got, err := testHandler.Queries.WithTx(tx).LockIdlePlatformExtensionRuntime(
				context.Background(),
				db.LockIdlePlatformExtensionRuntimeParams{
					WorkspaceID:      parseUUID(workspaceID),
					EligibleIds:      []pgtype.UUID{parseUUID(runtimeID)},
					UseRedisLiveness: tt.redisLiveness,
				},
			)
			if tt.wantRuntime {
				if err != nil {
					t.Fatalf("LockIdlePlatformExtensionRuntime() error = %v", err)
				}
				if uuidToString(got.ID) != runtimeID {
					t.Fatalf("selected runtime = %s, want %s", uuidToString(got.ID), runtimeID)
				}
				return
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("LockIdlePlatformExtensionRuntime() error = %v, want pgx.ErrNoRows", err)
			}
		})
	}
}

func TestLockIdlePlatformExtensionRuntimeTreatsEveryPendingStatusAsBusy(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	for _, status := range []string{"queued", "deferred", "dispatched", "running", "waiting_local_directory"} {
		t.Run(status, func(t *testing.T) {
			workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
			runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
				Provider:   "platform-agent-cli",
				Status:     "online",
				LastSeenAt: time.Now(),
				Visibility: "public",
				OwnerID:    testUserID,
			})
			createPlatformExtensionBusyTask(t, workspaceID, runtimeID, status)

			tx, err := testPool.Begin(context.Background())
			if err != nil {
				t.Fatalf("begin allocation transaction: %v", err)
			}
			defer tx.Rollback(context.Background())

			_, err = testHandler.Queries.WithTx(tx).LockIdlePlatformExtensionRuntime(
				context.Background(),
				db.LockIdlePlatformExtensionRuntimeParams{
					WorkspaceID:      parseUUID(workspaceID),
					EligibleIds:      []pgtype.UUID{parseUUID(runtimeID)},
					UseRedisLiveness: true,
				},
			)
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("status %q selected a busy runtime: error = %v", status, err)
			}
		})
	}
}

func TestLockIdlePlatformExtensionRuntimeUsesDeterministicOrder(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Run("newest last seen wins before agent count", func(t *testing.T) {
		workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
		older := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now().Add(-time.Minute), Visibility: "public", OwnerID: testUserID,
		})
		newer := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "public", OwnerID: testUserID,
		})
		createPlatformExtensionTestAgent(t, workspaceID, newer, "newer-runtime-agent")

		got := lockPlatformExtensionRuntimeForTest(t, workspaceID, []string{older, newer}, true)
		if uuidToString(got.ID) != newer {
			t.Fatalf("selected runtime = %s, want newest %s", uuidToString(got.ID), newer)
		}
	})

	t.Run("fewer active agents wins when heartbeat ties", func(t *testing.T) {
		workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
		seenAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
		loaded := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			Provider: "platform-agent-cli", Status: "online", LastSeenAt: seenAt, Visibility: "public", OwnerID: testUserID,
		})
		idle := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			Provider: "platform-agent-cli", Status: "online", LastSeenAt: seenAt, Visibility: "public", OwnerID: testUserID,
		})
		createPlatformExtensionTestAgent(t, workspaceID, loaded, "loaded-runtime-agent")

		got := lockPlatformExtensionRuntimeForTest(t, workspaceID, []string{loaded, idle}, true)
		if uuidToString(got.ID) != idle {
			t.Fatalf("selected runtime = %s, want least-bound %s", uuidToString(got.ID), idle)
		}
	})

	t.Run("oldest created runtime wins before id", func(t *testing.T) {
		workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
		seenAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
		newerCreated := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			ID: "00000000-0000-0000-0000-000000000001", Provider: "platform-agent-cli", Status: "online", LastSeenAt: seenAt,
			CreatedAt: time.Now(), Visibility: "public", OwnerID: testUserID,
		})
		olderCreated := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			ID: "ffffffff-ffff-ffff-ffff-ffffffffffff", Provider: "platform-agent-cli", Status: "online", LastSeenAt: seenAt,
			CreatedAt: time.Now().Add(-time.Hour), Visibility: "public", OwnerID: testUserID,
		})

		got := lockPlatformExtensionRuntimeForTest(t, workspaceID, []string{newerCreated, olderCreated}, true)
		if uuidToString(got.ID) != olderCreated {
			t.Fatalf("selected runtime = %s, want oldest-created %s", uuidToString(got.ID), olderCreated)
		}
	})

	t.Run("lowest id wins exact tie", func(t *testing.T) {
		workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
		seenAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
		createdAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
		highID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			ID: "ffffffff-ffff-ffff-ffff-ffffffffffff", Provider: "platform-agent-cli", Status: "online", LastSeenAt: seenAt,
			CreatedAt: createdAt, Visibility: "public", OwnerID: testUserID,
		})
		lowID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
			ID: "00000000-0000-0000-0000-000000000001", Provider: "platform-agent-cli", Status: "online", LastSeenAt: seenAt,
			CreatedAt: createdAt, Visibility: "public", OwnerID: testUserID,
		})

		got := lockPlatformExtensionRuntimeForTest(t, workspaceID, []string{highID, lowID}, true)
		if uuidToString(got.ID) != lowID {
			t.Fatalf("selected runtime = %s, want lowest id %s", uuidToString(got.ID), lowID)
		}
	})
}

func TestLockIdlePlatformExtensionRuntimeSkipsRowsLockedByConcurrentImport(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "owner")
	seenAt := time.Now().UTC().Truncate(time.Microsecond)
	firstID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		ID: "00000000-0000-0000-0000-000000000001", Provider: "platform-agent-cli", Status: "online", LastSeenAt: seenAt,
		CreatedAt: seenAt, Visibility: "public", OwnerID: testUserID,
	})
	secondID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		ID: "00000000-0000-0000-0000-000000000002", Provider: "platform-agent-cli", Status: "online", LastSeenAt: seenAt,
		CreatedAt: seenAt, Visibility: "public", OwnerID: testUserID,
	})
	eligible := []pgtype.UUID{parseUUID(firstID), parseUUID(secondID)}

	tx1, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin first import: %v", err)
	}
	defer tx1.Rollback(context.Background())
	first, err := testHandler.Queries.WithTx(tx1).LockIdlePlatformExtensionRuntime(
		context.Background(),
		db.LockIdlePlatformExtensionRuntimeParams{
			WorkspaceID: parseUUID(workspaceID), EligibleIds: eligible, UseRedisLiveness: true,
		},
	)
	if err != nil {
		t.Fatalf("first allocation: %v", err)
	}
	if uuidToString(first.ID) != firstID {
		t.Fatalf("first allocation = %s, want %s", uuidToString(first.ID), firstID)
	}

	tx2, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin concurrent import: %v", err)
	}
	defer tx2.Rollback(context.Background())
	second, err := testHandler.Queries.WithTx(tx2).LockIdlePlatformExtensionRuntime(
		context.Background(),
		db.LockIdlePlatformExtensionRuntimeParams{
			WorkspaceID: parseUUID(workspaceID), EligibleIds: eligible, UseRedisLiveness: true,
		},
	)
	if err != nil {
		t.Fatalf("concurrent allocation: %v", err)
	}
	if uuidToString(second.ID) != secondID {
		t.Fatalf("concurrent allocation = %s, want unlocked runtime %s", uuidToString(second.ID), secondID)
	}
}

func TestEligiblePlatformExtensionRuntimeIDsReuseRuntimeVisibilityPermissions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name       string
		role       string
		visibility string
		owned      bool
		want       bool
	}{
		{name: "member may use public runtime", role: "member", visibility: "public", want: true},
		{name: "member may not use another owner's private runtime", role: "member", visibility: "private", want: false},
		{name: "member may use own private runtime", role: "member", visibility: "private", owned: true, want: true},
		{name: "owner may use private runtime", role: "owner", visibility: "private", want: true},
		{name: "admin may use private runtime", role: "admin", visibility: "private", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceID := createPlatformExtensionTestWorkspace(t, tt.role)
			ownerID := createPlatformExtensionOtherUser(t)
			if tt.owned {
				ownerID = testUserID
			}
			runtimeID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
				Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(),
				Visibility: tt.visibility, OwnerID: ownerID,
			})
			member := platformExtensionTestMember(t, workspaceID)
			h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{
				alive: map[string]bool{runtimeID: true}, ok: true,
			})

			ids, useRedis, err := h.eligiblePlatformExtensionRuntimeIDs(context.Background(), member, parseUUID(workspaceID))
			if err != nil {
				t.Fatalf("eligiblePlatformExtensionRuntimeIDs() error = %v", err)
			}
			if !useRedis {
				t.Fatal("healthy liveness store did not select Redis mode")
			}
			got := len(ids) == 1 && uuidToString(ids[0]) == runtimeID
			if got != tt.want {
				t.Fatalf("eligible ids = %v, want runtime eligible = %v", ids, tt.want)
			}
		})
	}
}

func TestEligiblePlatformExtensionRuntimeIDsUseAliveSetOrDatabaseFallback(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID := createPlatformExtensionTestWorkspace(t, "member")
	aliveID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "public", OwnerID: createPlatformExtensionOtherUser(t),
	})
	deadID := createPlatformExtensionTestRuntime(t, workspaceID, platformExtensionRuntimeSeed{
		Provider: "platform-agent-cli", Status: "online", LastSeenAt: time.Now(), Visibility: "public", OwnerID: createPlatformExtensionOtherUser(t),
	})
	member := platformExtensionTestMember(t, workspaceID)

	t.Run("healthy Redis keeps only alive ids", func(t *testing.T) {
		h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{
			alive: map[string]bool{aliveID: true, deadID: false}, ok: true,
		})
		ids, useRedis, err := h.eligiblePlatformExtensionRuntimeIDs(context.Background(), member, parseUUID(workspaceID))
		if err != nil {
			t.Fatalf("eligiblePlatformExtensionRuntimeIDs() error = %v", err)
		}
		if !useRedis {
			t.Fatal("healthy Redis returned database fallback mode")
		}
		if got := platformExtensionUUIDStrings(ids); len(got) != 1 || got[0] != aliveID {
			t.Fatalf("eligible ids = %v, want [%s]", got, aliveID)
		}
	})

	t.Run("Redis failure preserves authorized candidates for database freshness check", func(t *testing.T) {
		h := platformExtensionHandlerWithLiveness(platformExtensionFakeLiveness{ok: false})
		ids, useRedis, err := h.eligiblePlatformExtensionRuntimeIDs(context.Background(), member, parseUUID(workspaceID))
		if err != nil {
			t.Fatalf("eligiblePlatformExtensionRuntimeIDs() error = %v", err)
		}
		if useRedis {
			t.Fatal("failed Redis did not select database fallback mode")
		}
		got := platformExtensionUUIDStrings(ids)
		if len(got) != 2 || !containsPlatformExtensionString(got, aliveID) || !containsPlatformExtensionString(got, deadID) {
			t.Fatalf("eligible ids = %v, want both authorized candidates", got)
		}
	})

	t.Run("unconfigured store uses database fallback", func(t *testing.T) {
		h := platformExtensionHandlerWithLiveness(NewNoopLivenessStore())
		ids, useRedis, err := h.eligiblePlatformExtensionRuntimeIDs(context.Background(), member, parseUUID(workspaceID))
		if err != nil {
			t.Fatalf("eligiblePlatformExtensionRuntimeIDs() error = %v", err)
		}
		if useRedis || len(ids) != 2 {
			t.Fatalf("useRedis=%v ids=%v, want database fallback with two candidates", useRedis, ids)
		}
	})
}

type platformExtensionRuntimeSeed struct {
	ID         string
	Provider   string
	Status     string
	LastSeenAt time.Time
	CreatedAt  time.Time
	Visibility string
	OwnerID    string
}

type platformExtensionFakeLiveness struct {
	alive map[string]bool
	ok    bool
}

func (f platformExtensionFakeLiveness) Available() bool { return f.ok }

func (f platformExtensionFakeLiveness) Touch(context.Context, string, time.Duration) error {
	return nil
}

func (f platformExtensionFakeLiveness) IsAliveBatch(_ context.Context, ids []string) (map[string]bool, bool) {
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		result[id] = f.alive[id]
	}
	return result, f.ok
}

func (f platformExtensionFakeLiveness) Forget(context.Context, string) {}

func platformExtensionHandlerWithLiveness(store LivenessStore) *Handler {
	copy := *testHandler
	copy.LivenessStore = store
	return &copy
}

func createPlatformExtensionTestWorkspace(t *testing.T, role string) string {
	t.Helper()
	ctx := context.Background()
	var workspaceID string
	slug := "platform-extension-" + randomID()
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Platform Extension Test', $1, '', 'PEX')
		RETURNING id
	`, slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create extension test workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)
	`, workspaceID, testUserID, role); err != nil {
		t.Fatalf("create extension test member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM platform_extension_release WHERE workspace_id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	return workspaceID
}

func createPlatformExtensionTestRuntime(t *testing.T, workspaceID string, seed platformExtensionRuntimeSeed) string {
	t.Helper()
	if seed.ID == "" {
		seed.ID = "00000000-0000-0000-0000-" + randomID()[:12]
	}
	if seed.CreatedAt.IsZero() {
		seed.CreatedAt = time.Now()
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_runtime (
			id, workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at, created_at, updated_at, visibility
		) VALUES ($1, $2, NULL, $3, 'local', $4, $5, '', '{}'::jsonb, $6, $7, $8, $8, $9)
	`, seed.ID, workspaceID, "Platform Runtime "+seed.ID, seed.Provider, seed.Status, nullablePlatformExtensionID(seed.OwnerID), seed.LastSeenAt, seed.CreatedAt, seed.Visibility); err != nil {
		t.Fatalf("create extension test runtime: %v", err)
	}
	return seed.ID
}

func createPlatformExtensionOtherUser(t *testing.T) string {
	t.Helper()
	var userID string
	email := "platform-extension-" + randomID() + "@example.test"
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('Platform Extension Other User', $1) RETURNING id
	`, email).Scan(&userID); err != nil {
		t.Fatalf("create extension other user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func platformExtensionTestMember(t *testing.T, workspaceID string) db.Member {
	t.Helper()
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: parseUUID(testUserID), WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		t.Fatalf("load extension test member: %v", err)
	}
	return member
}

func createPlatformExtensionTestAgent(t *testing.T, workspaceID, runtimeID, name string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id, instructions,
			custom_env, custom_args
		) VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, name+"-"+randomID(), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create extension test agent: %v", err)
	}
	return agentID
}

func createPlatformExtensionBusyTask(t *testing.T, workspaceID, runtimeID, status string) {
	t.Helper()
	agentID := createPlatformExtensionTestAgent(t, workspaceID, runtimeID, "busy-agent")
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id, position, number
		) VALUES ($1, $2, 'backlog', 'none', 'member', $3, 0, 1)
		RETURNING id
	`, workspaceID, "Busy runtime issue "+randomID(), testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create busy runtime issue: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status)
		VALUES ($1, $2, $3, $4)
	`, agentID, issueID, runtimeID, status); err != nil {
		t.Fatalf("create %s runtime task: %v", status, err)
	}
}

func lockPlatformExtensionRuntimeForTest(t *testing.T, workspaceID string, runtimeIDs []string, useRedisLiveness bool) db.AgentRuntime {
	t.Helper()
	tx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin allocation transaction: %v", err)
	}
	defer tx.Rollback(context.Background())
	eligible := make([]pgtype.UUID, len(runtimeIDs))
	for i, runtimeID := range runtimeIDs {
		eligible[i] = parseUUID(runtimeID)
	}
	runtime, err := testHandler.Queries.WithTx(tx).LockIdlePlatformExtensionRuntime(
		context.Background(),
		db.LockIdlePlatformExtensionRuntimeParams{
			WorkspaceID: workspaceIDToUUID(workspaceID), EligibleIds: eligible, UseRedisLiveness: useRedisLiveness,
		},
	)
	if err != nil {
		t.Fatalf("LockIdlePlatformExtensionRuntime() error = %v", err)
	}
	return runtime
}

func workspaceIDToUUID(workspaceID string) pgtype.UUID {
	return parseUUID(workspaceID)
}

func nullablePlatformExtensionID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func platformExtensionUUIDStrings(ids []pgtype.UUID) []string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = uuidToString(id)
	}
	return values
}

func containsPlatformExtensionString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func platformExtensionRuntimeIDs(runtimes []db.AgentRuntime) []string {
	ids := make([]string, len(runtimes))
	for i, runtime := range runtimes {
		ids[i] = uuidToString(runtime.ID)
	}
	return ids
}

func describePlatformExtensionRuntime(runtime db.AgentRuntime) string {
	return fmt.Sprintf("%s/%s/%s", uuidToString(runtime.ID), runtime.Provider, runtime.Status)
}
