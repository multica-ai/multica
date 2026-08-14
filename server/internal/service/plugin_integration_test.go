package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil/plugintest"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

func repackPluginArchive(t *testing.T, archive []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("open Plugin archive: %v", err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	if err := writer.SetComment("semantically equivalent test package"); err != nil {
		t.Fatalf("set Plugin archive comment: %v", err)
	}
	for _, file := range reader.File {
		source, openErr := file.Open()
		if openErr != nil {
			t.Fatalf("open Plugin archive entry %s: %v", file.Name, openErr)
		}
		header := file.FileHeader
		destination, createErr := writer.CreateHeader(&header)
		if createErr != nil {
			source.Close() //nolint:errcheck
			t.Fatalf("create Plugin archive entry %s: %v", file.Name, createErr)
		}
		if _, copyErr := io.Copy(destination, source); copyErr != nil {
			source.Close() //nolint:errcheck
			t.Fatalf("copy Plugin archive entry %s: %v", file.Name, copyErr)
		}
		if closeErr := source.Close(); closeErr != nil {
			t.Fatalf("close Plugin archive entry %s: %v", file.Name, closeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close Plugin archive: %v", err)
	}
	return output.Bytes()
}

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
	var userID, workspaceID, isolationWorkspaceID, runtimeID, agentID, issueID string
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
		cleanupPluginWorkspace := func(id string) {
			pool.Exec(cleanup, `DELETE FROM plugin_execution_manifest WHERE workspace_id = $1`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_health WHERE workspace_id = $1`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_capability_snapshot WHERE workspace_id = $1`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_workspace_capability_state WHERE workspace_id = $1`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_binding WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_grant WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_installation WHERE workspace_id = $1`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_contribution WHERE release_id IN (SELECT id FROM plugin_release WHERE plugin_id IN (SELECT id FROM plugin_identity WHERE owner_workspace_id = $1))`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_artifact_file WHERE release_id IN (SELECT id FROM plugin_release WHERE plugin_id IN (SELECT id FROM plugin_identity WHERE owner_workspace_id = $1))`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_release WHERE plugin_id IN (SELECT id FROM plugin_identity WHERE owner_workspace_id = $1)`, id)
			pool.Exec(cleanup, `DELETE FROM plugin_identity WHERE owner_workspace_id = $1`, id)
			pool.Exec(cleanup, `DELETE FROM activity_log WHERE workspace_id = $1`, id)
		}
		cleanupPluginWorkspace(workspaceID)
		if isolationWorkspaceID != "" {
			cleanupPluginWorkspace(isolationWorkspaceID)
			pool.Exec(cleanup, `DELETE FROM member WHERE workspace_id = $1`, isolationWorkspaceID)
			pool.Exec(cleanup, `DELETE FROM workspace WHERE id = $1`, isolationWorkspaceID)
		}
		pool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
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
	agentUUID := util.MustParseUUID(agentID)
	installation, err = pluginService.EnablePlugin(ctx, workspaceUUID, installation.ID, actorUUID, "agent", agentUUID)
	if err != nil || !installation.Enabled {
		t.Fatalf("enable agent scope: installation=%+v err=%v", installation, err)
	}
	agentTaskID := createTask(nil)
	agentBundles, _, _, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(agentTaskID))
	if err != nil || len(agentBundles) != 1 {
		t.Fatalf("agent-scoped Plugin missing from task: bundles=%d err=%v", len(agentBundles), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, agentTaskID); err != nil {
		t.Fatalf("complete agent-scoped task: %v", err)
	}
	installation, err = pluginService.DisablePlugin(ctx, workspaceUUID, installation.ID, actorUUID, "agent", agentUUID)
	if err != nil || installation.Enabled {
		t.Fatalf("disable last active binding: installation=%+v err=%v", installation, err)
	}
	retryTaskID := createTask(firstTaskID)
	retryBundles, _, retryManifest, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(retryTaskID))
	if err != nil || len(retryBundles) != 1 || retryManifest.SnapshotDigest != manifest.SnapshotDigest {
		t.Fatalf("retry did not inherit pinned manifest: bundles=%d manifest=%+v err=%v", len(retryBundles), retryManifest, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, retryTaskID); err != nil {
		t.Fatalf("complete retry task: %v", err)
	}
	if _, err := pluginService.RollbackPlugin(ctx, workspaceUUID, installation.ID, actorUUID, "1.0.0"); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	privateSource := filepath.Join("..", "..", "..", "examples", "plugins", "incident-triage")
	if _, err := pluginService.InstallPrivateArchive(ctx, workspaceUUID, actorUUID, []byte("not a Plugin archive")); err == nil {
		t.Fatal("invalid private Plugin archive unexpectedly installed")
	} else {
		var pluginErr *PluginError
		if !errors.As(err, &pluginErr) || pluginErr.Kind != PluginErrorInvalid {
			t.Fatalf("invalid private Plugin error = %v", err)
		}
	}
	privateArchiveV1, _, err := plugincontract.PackDirectory(privateSource)
	if err != nil {
		t.Fatalf("pack private Plugin v1: %v", err)
	}
	privateInstallation, err := pluginService.InstallPrivateArchive(ctx, workspaceUUID, actorUUID, privateArchiveV1)
	if err != nil {
		t.Fatalf("install private Plugin v1: %v", err)
	}
	if privateInstallation.Enabled || privateInstallation.SourceKind != plugincontract.SourcePrivateDev {
		t.Fatalf("private install must be disabled and private: %+v", privateInstallation)
	}
	privateInstallation, err = pluginService.EnablePlugin(ctx, workspaceUUID, privateInstallation.ID, actorUUID, "agent", agentUUID)
	if err != nil {
		t.Fatalf("enable private Plugin: %v", err)
	}
	privateV1TaskID := createTask(nil)
	privateV1Bundles, _, privateV1Manifest, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(privateV1TaskID))
	if err != nil || len(privateV1Bundles) != 1 || privateV1Manifest == nil || !strings.Contains(privateV1Bundles[0].Content, "# Incident triage") {
		t.Fatalf("private v1 Skill missing from real task: bundles=%d manifest=%+v err=%v", len(privateV1Bundles), privateV1Manifest, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, privateV1TaskID); err != nil {
		t.Fatalf("complete private v1 task: %v", err)
	}

	privateV2Source := filepath.Join(t.TempDir(), "incident-triage")
	privateV2SkillDir := filepath.Join(privateV2Source, "skills", "incident-triage")
	if err := os.MkdirAll(privateV2SkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(privateSource, plugincontract.ManifestFilename))
	if err != nil {
		t.Fatal(err)
	}
	var privateManifest plugincontract.Manifest
	if err := json.Unmarshal(manifestBytes, &privateManifest); err != nil {
		t.Fatal(err)
	}
	privateManifest.Metadata.Version = "0.2.0"
	manifestBytes, err = json.MarshalIndent(privateManifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(privateV2Source, plugincontract.ManifestFilename), append(manifestBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	v2Skill := "---\nname: incident-triage\ndescription: Structure incident intake, severity assessment, evidence capture, and handoff.\n---\n\n# Incident triage v2\n\nUse the updated private workflow.\n"
	if err := os.WriteFile(filepath.Join(privateV2SkillDir, "SKILL.md"), []byte(v2Skill), 0o644); err != nil {
		t.Fatal(err)
	}
	privateArchiveV2, _, err := plugincontract.PackDirectory(privateV2Source)
	if err != nil {
		t.Fatalf("pack private Plugin v2: %v", err)
	}
	privateInstallation, err = pluginService.InstallPrivateArchive(ctx, workspaceUUID, actorUUID, privateArchiveV2)
	if err != nil {
		t.Fatalf("upgrade private Plugin: %v", err)
	}
	idempotentInstallation, err := pluginService.InstallPrivateArchive(ctx, workspaceUUID, actorUUID, privateArchiveV2)
	if err != nil || idempotentInstallation.ID != privateInstallation.ID {
		t.Fatalf("idempotent private upload: installation=%+v err=%v", idempotentInstallation, err)
	}
	repackedArchive := repackPluginArchive(t, privateArchiveV2)
	if bytes.Equal(repackedArchive, privateArchiveV2) {
		t.Fatal("repacked Private Plugin archive unexpectedly has identical bytes")
	}
	repackedInstallation, err := pluginService.InstallPrivateArchive(ctx, workspaceUUID, actorUUID, repackedArchive)
	if err != nil || repackedInstallation.ID != privateInstallation.ID {
		t.Fatalf("semantically equivalent private upload: installation=%+v err=%v", repackedInstallation, err)
	}
	if _, err := pluginService.InstallPrivateArchive(ctx, workspaceUUID, actorUUID, privateArchiveV1); err == nil {
		t.Fatal("private upload silently downgraded the installed release")
	} else {
		var pluginErr *PluginError
		if !errors.As(err, &pluginErr) || pluginErr.Kind != PluginErrorConflict || !strings.Contains(pluginErr.Message, "rollback") {
			t.Fatalf("private downgrade error = %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(privateV2SkillDir, "SKILL.md"), []byte(v2Skill+"\nConflicting content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflictingArchive, _, err := plugincontract.PackDirectory(privateV2Source)
	if err != nil {
		t.Fatalf("pack conflicting private Plugin: %v", err)
	}
	if _, err := pluginService.InstallPrivateArchive(ctx, workspaceUUID, actorUUID, conflictingArchive); err == nil {
		t.Fatal("changed content reused an immutable private Plugin version")
	} else {
		var pluginErr *PluginError
		if !errors.As(err, &pluginErr) || pluginErr.Kind != PluginErrorConflict {
			t.Fatalf("immutable private Plugin conflict error = %v", err)
		}
	}
	privateV2TaskID := createTask(nil)
	privateV2Bundles, _, _, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(privateV2TaskID))
	if err != nil || len(privateV2Bundles) != 1 || !strings.Contains(privateV2Bundles[0].Content, "# Incident triage v2") {
		t.Fatalf("private v2 Skill missing from new task: bundles=%d err=%v", len(privateV2Bundles), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, privateV2TaskID); err != nil {
		t.Fatalf("complete private v2 task: %v", err)
	}
	privateInstallation, err = pluginService.RollbackPlugin(ctx, workspaceUUID, privateInstallation.ID, actorUUID, "0.1.0")
	if err != nil {
		t.Fatalf("rollback private Plugin: %v", err)
	}
	if privateInstallation.SourceKind != plugincontract.SourcePrivateDev {
		t.Fatalf("rollback changed source kind: %+v", privateInstallation)
	}
	privateRollbackTaskID := createTask(nil)
	privateRollbackBundles, _, _, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(privateRollbackTaskID))
	if err != nil || len(privateRollbackBundles) != 1 || strings.Contains(privateRollbackBundles[0].Content, "# Incident triage v2") {
		t.Fatalf("private rollback did not restore v1 for new task: bundles=%d err=%v", len(privateRollbackBundles), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, privateRollbackTaskID); err != nil {
		t.Fatalf("complete private rollback task: %v", err)
	}
	privateV2Release, err := queries.GetPluginReleaseByVersion(ctx, db.GetPluginReleaseByVersionParams{
		PluginID: privateInstallation.PluginID,
		Version:  "0.2.0",
	})
	if err != nil {
		t.Fatalf("load private v2 release: %v", err)
	}
	if _, err := queries.RevokePluginRelease(ctx, db.RevokePluginReleaseParams{
		RevocationStatus: "revoked",
		RevocationReason: pgtype.Text{String: "integration test", Valid: true},
		ID:               privateV2Release.ID,
	}); err != nil {
		t.Fatalf("revoke private v2 release: %v", err)
	}
	if _, err := pluginService.RollbackPlugin(ctx, workspaceUUID, privateInstallation.ID, actorUUID, "0.2.0"); err == nil {
		t.Fatal("rollback selected a revoked Private Plugin release")
	} else {
		var pluginErr *PluginError
		if !errors.As(err, &pluginErr) || pluginErr.Kind != PluginErrorNotFound {
			t.Fatalf("revoked rollback error = %v", err)
		}
	}
	if _, err := pluginService.InstallPrivateArchive(ctx, workspaceUUID, actorUUID, privateArchiveV2); err == nil {
		t.Fatal("private upload reactivated a revoked release")
	} else {
		var pluginErr *PluginError
		if !errors.As(err, &pluginErr) || pluginErr.Kind != PluginErrorConflict || !strings.Contains(pluginErr.Message, "revoked") {
			t.Fatalf("revoked private upload error = %v", err)
		}
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Plugin Isolation', $1, '', 'PI2') RETURNING id
	`, fmt.Sprintf("plugin-isolation-%d", suffix)).Scan(&isolationWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, isolationWorkspaceID, userID); err != nil {
		t.Fatal(err)
	}
	isolationInstallation, err := pluginService.InstallPrivateArchive(ctx, util.MustParseUUID(isolationWorkspaceID), actorUUID, privateArchiveV1)
	if err != nil {
		t.Fatalf("install same private key in isolated workspace: %v", err)
	}
	if isolationInstallation.PluginID == privateInstallation.PluginID {
		t.Fatal("private Plugin identity leaked across workspaces")
	}
	if _, err := pluginService.EnablePlugin(ctx, util.MustParseUUID(isolationWorkspaceID), privateInstallation.ID, actorUUID, "workspace", util.MustParseUUID(isolationWorkspaceID)); err == nil {
		t.Fatal("cross-workspace installation mutation unexpectedly succeeded")
	}

	if err := pluginService.UninstallPlugin(ctx, workspaceUUID, privateInstallation.ID, actorUUID); err != nil {
		t.Fatalf("uninstall private Plugin: %v", err)
	}
	historicalBundles, _, historicalManifest, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(privateV1TaskID))
	if err != nil || len(historicalBundles) != 1 || historicalManifest.SnapshotDigest != privateV1Manifest.SnapshotDigest {
		t.Fatalf("uninstall changed historical private task: bundles=%d manifest=%+v err=%v", len(historicalBundles), historicalManifest, err)
	}
	afterUninstallTaskID := createTask(nil)
	afterUninstallBundles, _, _, err := taskService.LoadTaskPluginSkillBundles(ctx, util.MustParseUUID(afterUninstallTaskID))
	if err != nil || len(afterUninstallBundles) != 0 {
		t.Fatalf("uninstalled private Plugin leaked into new task: bundles=%d err=%v", len(afterUninstallBundles), err)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM activity_log
		WHERE workspace_id = $1 AND action LIKE 'plugin_private_%'
	`, workspaceID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 5 {
		t.Fatalf("private Plugin audit records = %d, want at least 5", auditCount)
	}
}
