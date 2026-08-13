package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil/plugintest"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestReferencePluginInstallEnablePinDisableAndRetry(t *testing.T) {
	ctx := context.Background()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	defer pool.Close()
	var pluginTablesExist bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('plugin_capability_snapshot') IS NOT NULL`).Scan(&pluginTablesExist); err != nil || !pluginTablesExist {
		t.Skip("plugin migrations are not applied")
	}

	suffix := time.Now().UnixNano()
	var userID, workspaceID, runtimeID, agentID, issueID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Plugin E2E', $1) RETURNING id`, fmt.Sprintf("plugin-e2e-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('Plugin E2E', $1, '', 'PE2') RETURNING id`, fmt.Sprintf("plugin-e2e-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'Plugin E2E', 'cloud', 'plugin_e2e', 'online', 'test', '{}'::jsonb, now(), 'private', $2) RETURNING id
	`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'Plugin E2E', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3) RETURNING id
	`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'Plugin E2E', 'in_progress', 'none', $2, 'member', $3, 0) RETURNING id
	`, workspaceID, userID, suffix%100000000).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		pool.Exec(cleanup, `DELETE FROM plugin_execution_manifest WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		pool.Exec(cleanup, `DELETE FROM plugin_health WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanup, `DELETE FROM plugin_capability_snapshot WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanup, `DELETE FROM plugin_workspace_capability_state WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanup, `DELETE FROM plugin_binding WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, workspaceID)
		pool.Exec(cleanup, `DELETE FROM plugin_grant WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, workspaceID)
		pool.Exec(cleanup, `DELETE FROM plugin_installation WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanup, `DELETE FROM issue WHERE id = $1`, issueID)
		pool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(cleanup, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanup, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(cleanup, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	queries := db.New(pool)
	pluginService := NewPluginService(queries, pool)
	taskService := NewTaskService(queries, pool, nil, nil)
	workspaceUUID := util.MustParseUUID(workspaceID)
	actorUUID := util.MustParseUUID(userID)
	release, err := plugintest.ReviewReadinessRelease()
	if err != nil {
		t.Fatalf("reference release: %v", err)
	}
	installation, err := pluginService.InstallPluginRelease(ctx, workspaceUUID, actorUUID, PluginReleasePublication{
		Release:       release,
		PublisherType: "official",
		TrustTier:     "official",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if installation.Enabled {
		t.Fatal("install must not implicitly enable plugin")
	}
	installation, err = pluginService.EnablePlugin(ctx, workspaceUUID, installation.ID, actorUUID, "workspace", workspaceUUID)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !installation.Enabled || installation.ActiveGeneration != installation.DesiredGeneration {
		t.Fatalf("installation not active: %+v", installation)
	}

	createTask := func(retryOf any) string {
		var taskID string
		err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, status, priority, context, runtime_id, retry_of_task_id)
			VALUES ($1, $2, 'queued', 0, '{}'::jsonb, $3, $4) RETURNING id
		`, agentID, issueID, runtimeID, retryOf).Scan(&taskID)
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		return taskID
	}
	firstTaskID := createTask(nil)
	bundles, refs, manifest, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(firstTaskID))
	if err != nil {
		t.Fatalf("load pinned bundles: %v", err)
	}
	if len(bundles) != 1 || len(refs) != 1 || manifest == nil || !strings.Contains(bundles[0].Content, "# Review readiness") {
		t.Fatalf("unexpected enabled task plugin payload: bundles=%d refs=%d manifest=%+v", len(bundles), len(refs), manifest)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, firstTaskID); err != nil {
		t.Fatalf("complete first task: %v", err)
	}

	if _, err := pluginService.DisablePlugin(ctx, workspaceUUID, installation.ID, actorUUID, "workspace", workspaceUUID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	oldBundles, _, _, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(firstTaskID))
	if err != nil || len(oldBundles) != 1 {
		t.Fatalf("disable changed pinned task: bundles=%d err=%v", len(oldBundles), err)
	}
	newTaskID := createTask(nil)
	newBundles, _, _, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(newTaskID))
	if err != nil || len(newBundles) != 0 {
		t.Fatalf("disabled plugin leaked into new task: bundles=%d err=%v", len(newBundles), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, newTaskID); err != nil {
		t.Fatalf("complete disabled task: %v", err)
	}
	retryTaskID := createTask(firstTaskID)
	retryBundles, _, retryManifest, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(retryTaskID))
	if err != nil || len(retryBundles) != 1 || retryManifest.SnapshotDigest != manifest.SnapshotDigest {
		t.Fatalf("retry did not inherit pinned manifest: bundles=%d manifest=%+v err=%v", len(retryBundles), retryManifest, err)
	}
	if _, err := pluginService.RollbackPlugin(ctx, workspaceUUID, installation.ID, actorUUID, "1.0.0"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
}
