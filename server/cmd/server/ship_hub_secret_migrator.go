package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

// migrateShipHubSecrets is the one-shot startup task that moves any
// pre-Phase-2 plaintext github_token rows out of workspace.settings
// into the encrypted workspace_secret table.
//
// Safe to run on every boot — the SQL probe filters out rows that have
// already been migrated. Errors are logged per-row so a single
// malformed JSON blob doesn't stall the whole sweep.
func migrateShipHubSecrets(ctx context.Context, queries *db.Queries) {
	rows, err := queries.ListWorkspacesNeedingSecretMigration(ctx)
	if err != nil {
		slog.Warn("ship hub secret migrator: list failed", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	slog.Info("ship hub secret migrator: migrating workspaces", "count", len(rows))
	for _, row := range rows {
		token := handler.ReadShipHubGitHubTokenForReconciler(row.Settings)
		if token == "" {
			// The settings probe filtered for rows with a token, but if
			// the JSON parse fails inside the helper we can still hit
			// this — clear the orphaned field anyway so we don't loop.
			_ = queries.ClearShipHubTokenInSettings(ctx, row.ID)
			continue
		}
		ciphertext, err := secrets.EncryptString(token)
		if err != nil {
			slog.Warn("ship hub secret migrator: encrypt failed",
				"workspace_id", row.ID, "error", err)
			continue
		}
		if _, err := queries.UpsertWorkspaceSecret(ctx, db.UpsertWorkspaceSecretParams{
			WorkspaceID:    row.ID,
			Name:           "github_token",
			ValueEncrypted: ciphertext,
		}); err != nil {
			slog.Warn("ship hub secret migrator: upsert failed",
				"workspace_id", row.ID, "error", err)
			continue
		}
		if err := queries.ClearShipHubTokenInSettings(ctx, row.ID); err != nil {
			slog.Warn("ship hub secret migrator: clear settings failed",
				"workspace_id", row.ID, "error", err)
			// We don't roll back the encrypted write — both stores
			// holding the value momentarily is harmless.
			continue
		}
		slog.Info("ship hub secret migrator: migrated", "workspace_id", row.ID)
	}

	// Forward-compat sanity check: if any settings rows still parse as
	// having a ship_hub.github_token field that wasn't picked up above,
	// log a warning so we have a forensic record. Should never fire on
	// a clean migration but cheap to verify.
	suspicious := 0
	if rows2, err := queries.ListWorkspacesNeedingSecretMigration(ctx); err == nil {
		for _, r := range rows2 {
			var probe struct {
				ShipHub struct {
					GithubToken string `json:"github_token"`
				} `json:"ship_hub"`
			}
			if err := json.Unmarshal(r.Settings, &probe); err == nil && probe.ShipHub.GithubToken != "" {
				suspicious++
			}
		}
	}
	if suspicious > 0 {
		slog.Warn("ship hub secret migrator: workspaces still report a token after migration",
			"count", suspicious)
	}
}
