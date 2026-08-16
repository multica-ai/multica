package db_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

func TestPluginLifecycleQueries(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres plugin lifecycle test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	var schemaReady bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('plugin_identity') IS NOT NULL`).Scan(&schemaReady); err != nil {
		t.Fatalf("check plugin schema: %v", err)
	}
	if !schemaReady {
		t.Skip("plugin lifecycle migrations are not applied")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	queries := db.New(tx)

	suffix := uuid.NewString()
	var workspaceID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'PLG')
		RETURNING id
	`, "Plugin lifecycle query test", "plugin-query-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	identity, err := queries.CreatePluginIdentity(ctx, db.CreatePluginIdentityParams{
		PluginKey:     "test.multica.plugin-" + suffix,
		DisplayName:   "Plugin Query Test",
		PublisherID:   "multica-test",
		PublisherType: "official",
		TrustTier:     "official",
	})
	if err != nil {
		t.Fatalf("CreatePluginIdentity: %v", err)
	}

	digest := func(value string) string { return plugincontract.DigestBytes([]byte(value)) }
	release, err := queries.CreatePluginRelease(ctx, db.CreatePluginReleaseParams{
		Version:        "1.0.0",
		Manifest:       []byte(`{}`),
		ManifestDigest: digest("manifest"),
		SourceKind:     plugincontract.SourceBundled,
		SourceRef:      "embedded://plugin-query-test/1.0.0",
		ArchiveDigest:  digest("archive"),
		ArtifactRef:    "cas://plugin-query-test/1.0.0",
		ArtifactDigest: digest("artifact"),
		ArtifactSize:   128,
		Signature:      make([]byte, 64),
		SignatureKeyID: pgtype.Text{String: "test-release-key", Valid: true},
		PluginID:       identity.ID,
	})
	if err != nil {
		t.Fatalf("CreatePluginRelease: %v", err)
	}

	contribution, err := queries.CreatePluginContribution(ctx, db.CreatePluginContributionParams{
		ContributionKey: "review-readiness",
		Type:            plugincontract.ContributionAgentSkillV1,
		SchemaVersion:   1,
		DisplayName:     "Review Readiness",
		Description:     "Review evidence",
		EntryPath:       "skills/review-readiness/SKILL.md",
		EntryDigest:     digest("entry"),
		RequiredDaemonFeatures: []byte(`[
			"execution-manifest-v1",
			"agent-skill-v1"
		]`),
		Ordinal:   0,
		ReleaseID: release.ID,
	})
	if err != nil {
		t.Fatalf("CreatePluginContribution: %v", err)
	}

	installation, err := queries.CreatePluginInstallation(ctx, db.CreatePluginInstallationParams{
		PluginID:    identity.ID,
		ReleaseID:   release.ID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("CreatePluginInstallation: %v", err)
	}
	if installation.Enabled || installation.ActiveGeneration != 0 || installation.LifecycleStatus != "installed" {
		t.Fatalf("new installation is not disabled: %#v", installation)
	}

	firstGrant, err := queries.CreatePluginGrantRevision(ctx, db.CreatePluginGrantRevisionParams{
		Capability:     plugincontract.CapabilityAgentSkillContribute,
		Decision:       "granted",
		Limits:         []byte(`{}`),
		InstallationID: installation.ID,
		WorkspaceID:    workspaceID,
	})
	if err != nil {
		t.Fatalf("CreatePluginGrantRevision first: %v", err)
	}
	secondGrant, err := queries.CreatePluginGrantRevision(ctx, db.CreatePluginGrantRevisionParams{
		Capability:     plugincontract.CapabilityAgentSkillContribute,
		Decision:       "denied",
		Limits:         []byte(`{}`),
		InstallationID: installation.ID,
		WorkspaceID:    workspaceID,
	})
	if err != nil {
		t.Fatalf("CreatePluginGrantRevision second: %v", err)
	}
	if firstGrant.GrantRevision != 1 || secondGrant.GrantRevision != 2 {
		t.Fatalf("grant revisions = %d, %d", firstGrant.GrantRevision, secondGrant.GrantRevision)
	}

	firstBinding, err := queries.CreatePluginBindingRevision(ctx, db.CreatePluginBindingRevisionParams{
		ScopeType:      "workspace",
		ScopeID:        workspaceID,
		Enabled:        true,
		InstallationID: installation.ID,
		WorkspaceID:    workspaceID,
	})
	if err != nil {
		t.Fatalf("CreatePluginBindingRevision first: %v", err)
	}
	secondBinding, err := queries.CreatePluginBindingRevision(ctx, db.CreatePluginBindingRevisionParams{
		ScopeType:      "workspace",
		ScopeID:        workspaceID,
		Enabled:        false,
		InstallationID: installation.ID,
		WorkspaceID:    workspaceID,
	})
	if err != nil {
		t.Fatalf("CreatePluginBindingRevision second: %v", err)
	}
	if firstBinding.BindingRevision != 1 || secondBinding.BindingRevision != 2 {
		t.Fatalf("binding revisions = %d, %d", firstBinding.BindingRevision, secondBinding.BindingRevision)
	}

	oauthStateHash := make([]byte, 32)
	copy(oauthStateHash, []byte(suffix))
	if _, err := tx.Exec(ctx, `
		INSERT INTO plugin_remote_mcp_oauth_state (
			state_hash, workspace_id, installation_id, contribution_id, actor_id,
			endpoint, authorization_endpoint, token_endpoint, client_id,
			redirect_uri, secret_ciphertext, expires_at
		)
		VALUES (
			$1, $2, $3, $4, $2,
			'https://mcp.example/mcp', 'https://mcp.example/authorize',
			'https://mcp.example/token', 'plugin-query-test',
			'https://multica.example/oauth/callback', $5, now() + interval '10 minutes'
		)
	`, oauthStateHash, workspaceID, installation.ID, contribution.ID, []byte("encrypted-state")); err != nil {
		t.Fatalf("create Remote MCP OAuth state: %v", err)
	}

	updated, err := queries.SetPluginInstallationDesiredState(ctx, db.SetPluginInstallationDesiredStateParams{
		WorkspaceID:      workspaceID,
		DesiredReleaseID: release.ID,
		ID:               installation.ID,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("SetPluginInstallationDesiredState: %v", err)
	}
	if !updated.Enabled || updated.DesiredGeneration != 2 || updated.ActiveGeneration != 0 || updated.LifecycleStatus != "activating" {
		t.Fatalf("desired state update = %#v", updated)
	}

	if _, err := tx.Exec(ctx, `SAVEPOINT immutable_release`); err != nil {
		t.Fatalf("savepoint immutable release: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE plugin_release SET version = '1.0.1' WHERE id = $1`, release.ID); err == nil {
		t.Fatal("immutable release update succeeded")
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT immutable_release`); err != nil {
		t.Fatalf("rollback immutable release savepoint: %v", err)
	}

	if _, err := tx.Exec(ctx, `SAVEPOINT append_only_contribution`); err != nil {
		t.Fatalf("savepoint append-only contribution: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE plugin_contribution SET display_name = 'Changed' WHERE id = $1`, contribution.ID); err == nil {
		t.Fatal("append-only contribution update succeeded")
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT append_only_contribution`); err != nil {
		t.Fatalf("rollback append-only contribution savepoint: %v", err)
	}

	if err := queries.DeleteWorkspacePluginData(ctx, workspaceID); err != nil {
		t.Fatalf("DeleteWorkspacePluginData: %v", err)
	}
	if _, err := queries.GetPluginInstallation(ctx, installation.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("installation after workspace cleanup error = %v", err)
	}
	var oauthStateCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM plugin_remote_mcp_oauth_state WHERE workspace_id = $1
	`, workspaceID).Scan(&oauthStateCount); err != nil {
		t.Fatalf("count Remote MCP OAuth states after workspace cleanup: %v", err)
	}
	if oauthStateCount != 0 {
		t.Fatalf("Remote MCP OAuth states after workspace cleanup = %d, want 0", oauthStateCount)
	}
	remainingContributions, err := queries.ListPluginContributionsByRelease(ctx, release.ID)
	if err != nil {
		t.Fatalf("ListPluginContributionsByRelease: %v", err)
	}
	if len(remainingContributions) != 1 {
		t.Fatalf("global contributions after workspace cleanup = %d, want 1", len(remainingContributions))
	}

	revoked, err := queries.RevokePluginRelease(ctx, db.RevokePluginReleaseParams{
		RevocationStatus: "revoked",
		RevocationReason: pgtype.Text{String: "integration test", Valid: true},
		ID:               release.ID,
	})
	if err != nil {
		t.Fatalf("RevokePluginRelease: %v", err)
	}
	if revoked.RevocationStatus != "revoked" || !revoked.RevokedAt.Valid {
		t.Fatalf("revoked release = %#v", revoked)
	}
	if _, err := queries.RevokePluginRelease(ctx, db.RevokePluginReleaseParams{
		RevocationStatus: "quarantined",
		RevocationReason: pgtype.Text{String: "second transition", Valid: true},
		ID:               release.ID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second release revocation error = %v", err)
	}
}
