package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func mustParseUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	parsed, err := util.ParseUUID(value)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	return parsed
}

// Workspace teardown is the one place plugin rows are removed by something
// other than an explicit uninstall, and there are no foreign keys or cascades
// by repository policy — so the sweep is application SQL that has to name every
// table itself. workspace_delete_manifest_test.go asserts the three tables are
// registered; this asserts the query actually empties them.
func TestDeleteWorkspacePluginDataClearsEveryPluginTable(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres plugin teardown test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	var schemaReady bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('plugin_installation') IS NOT NULL`).Scan(&schemaReady); err != nil {
		t.Fatalf("check plugin schema: %v", err)
	}
	if !schemaReady {
		t.Skip("plugin migrations are not applied")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	queries := db.New(tx)

	suffix := time.Now().UnixNano()
	var workspaceID string
	if err := tx.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('plugin teardown', $1) RETURNING id`,
		fmt.Sprintf("plugin-teardown-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	parsedWorkspaceID := mustParseUUID(t, workspaceID)

	manifest, _ := json.Marshal(map[string]any{"manifest_version": 1})
	installation, err := queries.CreatePluginInstallation(ctx, db.CreatePluginInstallationParams{
		WorkspaceID:   parsedWorkspaceID,
		PluginKey:     fmt.Sprintf("com.example.teardown%d", suffix),
		SourceUrl:     "local:fixture",
		Version:       "1.0.0",
		Manifest:      manifest,
		GrantedScopes: []byte(`["issues:read"]`),
	})
	if err != nil {
		t.Fatalf("create installation: %v", err)
	}

	if _, err := queries.UpsertPluginStorageValue(ctx, db.UpsertPluginStorageValueParams{
		InstallationID: installation.ID,
		ScopeType:      "workspace",
		ScopeID:        parsedWorkspaceID,
		Key:            "state",
		Value:          "kept",
	}); err != nil {
		t.Fatalf("seed storage: %v", err)
	}
	if err := queries.UpsertPluginSecret(ctx, db.UpsertPluginSecretParams{
		InstallationID: installation.ID,
		Key:            "token",
		Ciphertext:     []byte("ciphertext"),
	}); err != nil {
		t.Fatalf("seed secret: %v", err)
	}

	if err := queries.DeleteWorkspacePluginData(ctx, parsedWorkspaceID); err != nil {
		t.Fatalf("DeleteWorkspacePluginData: %v", err)
	}

	if _, err := queries.GetPluginInstallation(ctx, installation.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("installation survived workspace deletion: %v", err)
	}
	// The leaf tables are keyed by installation id, so a sweep that forgot them
	// would leave rows nothing can reach or clean up later.
	for _, table := range []string{"plugin_storage", "plugin_secret"} {
		var remaining int
		if err := tx.QueryRow(ctx,
			fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE installation_id = $1`, table),
			installation.ID,
		).Scan(&remaining); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if remaining != 0 {
			t.Fatalf("%s kept %d rows after workspace deletion", table, remaining)
		}
	}
}
