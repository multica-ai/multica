package handler

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestPersonalBrowserFeatureEnabled_UsesPersonalOverrideWhenWorkspaceIsUnset(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	var ownerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Personal Browser Owner', 'personal-browser-owner@multica.test')
		RETURNING id
	`).Scan(&ownerID); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID)
	})

	if _, err := testPool.Exec(ctx, `
		DELETE FROM cerebro_feature_flags
		WHERE workspace_id = $1 AND flag_key = $2
	`, testWorkspaceID, personalBrowserFeatureFlag); err != nil {
		t.Fatalf("clear browser flags: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			DELETE FROM cerebro_feature_flags
			WHERE workspace_id = $1 AND flag_key = $2
		`, testWorkspaceID, personalBrowserFeatureFlag)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
		VALUES ($1, $2, $3, true)
	`, testWorkspaceID, ownerID, personalBrowserFeatureFlag); err != nil {
		t.Fatalf("seed personal browser flag: %v", err)
	}

	env := testHandler.withPersonalBrowserEnv(
		ctx,
		nil,
		parseUUID(testWorkspaceID),
		parseUUID(ownerID),
	)
	if env[personalBrowserGrantEnv] != "1" {
		t.Fatalf("personal browser env = %q, want 1", env[personalBrowserGrantEnv])
	}
}

func TestDaemonClaimWiresPersonalBrowserEnvironment(t *testing.T) {
	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon.go: %v", err)
	}

	required := "customEnv = h.withPersonalBrowserEnv(r.Context(), customEnv, agent.WorkspaceID, agent.OwnerID)"
	if !strings.Contains(string(src), required) {
		t.Fatalf("daemon.go is missing personal-browser claim wiring %q", required)
	}
}
