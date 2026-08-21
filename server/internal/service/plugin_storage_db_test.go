package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The quota decision itself is covered by TestEnforceStorageQuota. What can
// only be pinned against a real database is the usage query underneath it: that
// it measures bytes rather than characters, and that excluding the candidate
// key makes an overwrite count as a replacement instead of an addition. Both
// are invisible to a unit test and both silently inflate the effective budget
// when wrong.

func newPluginStoragePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedPluginInstallation creates the one row the storage tables hang off. There
// are no foreign keys, but the service always operates through an installation
// id, so the test does too.
func seedPluginInstallation(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('plugin ws', $1) RETURNING id`,
		fmt.Sprintf("plugin-storage-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID) })

	manifest, _ := json.Marshal(map[string]any{"manifest_version": 1})
	// An installation names the published version it runs. This suite is about
	// the KV quotas, so the version id is a bare identifier with no package row
	// behind it — relationships are application-owned by repository policy, and
	// nothing on this path resolves it.
	var installationID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO plugin_installation (workspace_id, plugin_key, package_version_id, version, manifest)
		 VALUES ($1, $2, gen_random_uuid(), '1.0.0', $3) RETURNING id`,
		workspaceID, fmt.Sprintf("com.example.storage%d", suffix), manifest,
	).Scan(&installationID); err != nil {
		t.Fatalf("seed plugin installation: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM plugin_storage WHERE installation_id = $1`, installationID)
		pool.Exec(context.Background(), `DELETE FROM plugin_installation WHERE id = $1`, installationID)
	})

	parsed, err := util.ParseUUID(installationID)
	if err != nil {
		t.Fatalf("parse installation id: %v", err)
	}
	return parsed
}

func TestPluginStorageUsageCountsBytesAndExcludesTheCandidateKey(t *testing.T) {
	pool := newPluginStoragePool(t)
	ctx := context.Background()
	queries := db.New(pool)
	service := &PluginService{Queries: queries}

	installationID := seedPluginInstallation(t, pool)
	scopeID := installationID // any UUID: the scope is opaque to storage

	// 10 four-byte characters: 10 runes, 40 bytes.
	value := strings.Repeat("🙂", 10)
	if err := service.SetStorageValue(ctx, installationID, PluginStorageWorkspace, scopeID, "emoji", value); err != nil {
		t.Fatalf("SetStorageValue: %v", err)
	}

	usage, err := queries.GetPluginStorageUsage(ctx, db.GetPluginStorageUsageParams{
		InstallationID: installationID,
		ScopeType:      PluginStorageWorkspace,
		ScopeID:        scopeID,
		Key:            "other",
	})
	if err != nil {
		t.Fatalf("GetPluginStorageUsage: %v", err)
	}
	if usage.KeyCount != 1 {
		t.Fatalf("key_count = %d, want 1", usage.KeyCount)
	}
	if usage.TotalBytes != int64(len(value)) {
		t.Fatalf("total_bytes = %d, want %d (char_length would report %d)",
			usage.TotalBytes, len(value), len([]rune(value)))
	}

	// Same query with the stored key as the candidate: an overwrite must be
	// measured as a replacement, so the existing row drops out entirely.
	replacement, err := queries.GetPluginStorageUsage(ctx, db.GetPluginStorageUsageParams{
		InstallationID: installationID,
		ScopeType:      PluginStorageWorkspace,
		ScopeID:        scopeID,
		Key:            "emoji",
	})
	if err != nil {
		t.Fatalf("GetPluginStorageUsage (replacement): %v", err)
	}
	if replacement.KeyCount != 0 || replacement.TotalBytes != 0 {
		t.Fatalf("replacement usage = (%d keys, %d bytes), want (0, 0)", replacement.KeyCount, replacement.TotalBytes)
	}

	// The listing reports the same byte count the quota is spent against.
	keys, err := service.ListStorageKeys(ctx, installationID, PluginStorageWorkspace, scopeID)
	if err != nil {
		t.Fatalf("ListStorageKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].SizeBytes != int64(len(value)) {
		t.Fatalf("listed keys = %+v, want one key of %d bytes", keys, len(value))
	}
}

func TestPluginStorageRejectsAValueOverTheByteCap(t *testing.T) {
	pool := newPluginStoragePool(t)
	ctx := context.Background()
	service := &PluginService{Queries: db.New(pool)}

	installationID := seedPluginInstallation(t, pool)

	// Under the cap in characters, over it in bytes. Before the accounting was
	// byte-based this write was accepted by both the service and the column
	// constraint.
	oversized := strings.Repeat("🙂", MaxPluginStorageValueBytes/4+1)
	err := service.SetStorageValue(ctx, installationID, PluginStorageWorkspace, installationID, "big", oversized)
	if err == nil {
		t.Fatal("SetStorageValue accepted a value over the byte cap")
	}
	if kind := pluginErrKind(t, err); kind != PluginErrorQuota {
		t.Fatalf("kind = %q, want %q", kind, PluginErrorQuota)
	}
}

func TestPluginStorageScopesAreIsolated(t *testing.T) {
	pool := newPluginStoragePool(t)
	ctx := context.Background()
	service := &PluginService{Queries: db.New(pool)}

	installationID := seedPluginInstallation(t, pool)
	otherScope, err := util.ParseUUID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("parse scope id: %v", err)
	}

	if err := service.SetStorageValue(ctx, installationID, PluginStorageUser, installationID, "pref", "mine"); err != nil {
		t.Fatalf("SetStorageValue: %v", err)
	}
	// A different member's user scope must not see it — this is the guarantee
	// that makes storage:user safe to grant.
	if _, err := service.GetStorageValue(ctx, installationID, PluginStorageUser, otherScope, "pref"); err == nil {
		t.Fatal("a user-scoped value was readable from another member's scope")
	} else if kind := pluginErrKind(t, err); kind != PluginErrorNotFound {
		t.Fatalf("kind = %q, want %q", kind, PluginErrorNotFound)
	}
	// Nor may the workspace scope, which is a different scope_type entirely.
	if _, err := service.GetStorageValue(ctx, installationID, PluginStorageWorkspace, installationID, "pref"); err == nil {
		t.Fatal("a user-scoped value was readable from the workspace scope")
	}
}
