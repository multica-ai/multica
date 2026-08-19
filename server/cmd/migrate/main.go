package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/attributionbackfill"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/migrations"
	"github.com/multica-ai/multica/server/internal/taskusagebackfill"
)

// preMigrationHook runs work that must happen before a specific migration is
// applied in the direction whose hook map selected it. Hooks are idempotent and
// must not depend on the migration loop's session-pinned advisory lock
// — they run on the pool, not on the loop's pinned conn, so they can
// safely acquire other session-level locks (e.g. advisory lock 4246
// for the task_usage hourly rollup).
//
// Returning an error aborts the migration run. The corresponding migration is
// not added to (up) or removed from (down) schema_migrations, so the next run
// retries the hook + migration.
type preMigrationHook func(ctx context.Context, pool *pgxpool.Pool) error

// preMigrationHooks wires migration version → hook. The version key is
// the file basename without the `.up.sql` suffix, matching what
// `migrations.ExtractVersion` returns.
//
// MUL-2957: the v0.3.4 → current direct-upgrade path needs the hourly
// rollup seeded BEFORE migration 103 evaluates its fail-closed lag
// guard, because at `cmd/migrate up` time the server has not yet
// started so neither the legacy pg_cron job nor the new app scheduler
// can advance the watermark. The hook runs the same idempotent
// monthly-slice backfill that
// `cmd/backfill_task_usage_hourly` exposes to operators.
//
// MUL-4897 / GH #5544: migration 198 VALIDATEs the strict attribution
// constraint installed by 197, which drops migration 190's
// originator_source IS NULL exemption. Self-hosted databases never ran the
// out-of-band backfill that Multica's cloud did, so their legacy rows make
// 198 fail closed and the backend refuses to start. The hook reconciles
// those rows (accountable_user_id := originator_user_id) idempotently BEFORE
// VALIDATE, so a stuck-at-197 instance auto-heals on `migrate up` with no
// manual SQL. A higher-numbered migration cannot help — the instance never
// reaches a version above the failing 198.
//
// GH #6388: migration 257 builds a replacement unique index concurrently. A
// failed build can leave an INVALID relation that IF NOT EXISTS would otherwise
// mistake for a successful retry. The hook removes only that invalid leftover;
// migration 257 can then rebuild it while the valid v1 index remains in place.
//
// MUL-5823: migration 261 replaces the terminal-task partial index the same
// way, so it carries the same hazard — an INVALID v2 leftover recorded as
// success would let migration 262 drop the still-valid v1, leaving all four
// dashboard rollups on a full table scan.
// concurrentIndexCleanups maps a migration version to the index it builds with
// CREATE INDEX CONCURRENTLY. Every entry gets an invalid-index cleanup hook, so
// an interrupted build cannot be mistaken for success on retry.
//
// The mapping is data rather than individual hand-written hook registrations so a
// test can check each entry against the index its migration file actually
// creates — a typo here would be invisible at runtime, because a hook that names
// a nonexistent index is a silent no-op.
//
// MUL-5999: migrations 273–277 each build one index concurrently, three of them
// on hot tables (agent_task_queue is the largest table in the database). They
// carry the same hazard as 257 / 261: an interrupted build leaves an INVALID
// index of the same name, `IF NOT EXISTS` then skips the rebuild, the runner
// records the migration as applied, and the queries that need the index silently
// stay on a full scan — the exact regression these migrations exist to fix.
//
// MUL-6288: registration used to be opt-in per batch, so 316 / 317 / 326 / 328 /
// 330 / 331 shipped without a hook and the hazard came back. The map is now
// total — every up migration that builds an index concurrently is listed, the
// same invariant `concurrentDownIndexCleanups` already holds for rollbacks — and
// TestEveryConcurrentUpBuildHasCleanup fails the build if a new migration is
// added without its entry. Registering the already-applied historical
// migrations costs one to_regclass lookup each, and only on a database where
// they are still pending: a fresh self-hosted install, which is exactly where an
// interrupted build would otherwise leave a permanently unusable index.
var concurrentIndexCleanups = map[string]string{
	"035_task_queue_issue_id_index":                             "idx_agent_task_queue_issue_id",
	"067_task_queue_claim_candidate_index":                      "idx_agent_task_queue_claim_candidates",
	"074_task_usage_updated_at_index":                           "idx_task_usage_updated_at",
	"075_task_usage_created_at_index":                           "idx_task_usage_created_at",
	"078_task_usage_created_at_legacy_index":                    "idx_task_usage_created_at_legacy",
	"080_agent_task_queue_queued_index":                         "idx_agent_task_queue_queued_created_at",
	"106_member_user_workspace_index":                           "idx_member_user_workspace",
	"114_agent_task_queue_running_started_at_index":             "idx_agent_task_queue_running_started_at",
	"115_agent_runtime_last_seen_at_index":                      "idx_agent_runtime_last_seen_at",
	"119_user_created_at_index":                                 "idx_user_created_at",
	"125_agent_task_queue_dispatched_prepare_index":             "idx_agent_task_queue_dispatched_prepare",
	"135_comment_workspace_index":                               "idx_comment_workspace",
	"138_issue_title_trgm_index":                                "idx_issue_title_trgm",
	"139_issue_description_trgm_index":                          "idx_issue_description_trgm",
	"140_comment_content_trgm_index":                            "idx_comment_content_trgm",
	"141_project_title_trgm_index":                              "idx_project_title_trgm",
	"142_project_description_trgm_index":                        "idx_project_description_trgm",
	"143_agent_task_queue_chat_pending_v2":                      "idx_agent_task_queue_chat_pending_v2",
	"153_chat_pinned_agent_user_ws_index":                       "idx_chat_pinned_agent_user_ws",
	"156_chat_session_pinned_index":                             "idx_chat_session_pinned",
	"160_chat_message_input_owner_index":                        "idx_chat_message_input_owner",
	"165_attachment_task_id_index":                              "idx_attachment_task",
	"167_resource_label_namespace_index":                        "issue_label_workspace_type_name_lower_idx",
	"168_resource_label_type_index":                             "issue_label_workspace_type_idx",
	"169_agent_label_lookup_index":                              "agent_to_label_label_idx",
	"170_skill_label_lookup_index":                              "skill_to_label_label_idx",
	"172_agent_system_identity_index":                           "agent_system_identity_unique",
	"177_autopilot_run_webhook_delivery_index":                  "uq_autopilot_run_webhook_delivery",
	"178_webhook_delivery_queue_index":                          "idx_webhook_delivery_queue",
	"181_task_chat_finalize_deferred_index":                     "idx_task_chat_finalize_deferred",
	"183_chat_draft_restore_index":                              "idx_chat_draft_restore_session",
	"187_autopilot_rule_version_index":                          "idx_autopilot_rule_version_active",
	"192_issue_properties_gin_index":                            "idx_issue_properties_gin",
	"194_issue_property_workspace_name_index":                   "idx_issue_property_ws_name",
	"195_issue_property_workspace_index":                        "idx_issue_property_workspace",
	"200_inbox_archived_listing_index":                          "idx_inbox_recipient_archived_created",
	"201_inbox_active_by_issue_index":                           "idx_inbox_active_by_issue",
	"203_issue_workspace_assignee_index":                        "idx_issue_workspace_assignee",
	"204_issue_workspace_parent_index":                          "idx_issue_workspace_parent",
	"205_issue_workspace_position_index":                        "idx_issue_workspace_position",
	"208_client_usage_daily_unique_index":                       "client_usage_daily_identity_date_uidx",
	"210_client_usage_daily_query_index":                        "client_usage_daily_activity_client_user_idx",
	"211_client_usage_daily_workspace_index":                    "client_usage_daily_workspace_idx",
	"215_chat_session_project_index":                            "idx_chat_session_project",
	"217_vcs_connection_workspace_index":                        "idx_vcs_connection_workspace",
	"218_vcs_pull_request_workspace_index":                      "idx_vcs_pull_request_workspace",
	"219_vcs_pull_request_connection_index":                     "idx_vcs_pull_request_connection",
	"220_issue_vcs_pull_request_pr_index":                       "idx_issue_vcs_pull_request_pr",
	"221_vcs_commit_status_lookup_index":                        "idx_vcs_commit_status_lookup",
	"223_github_pr_check_run_pr_ordinal_index":                  "github_pull_request_check_run_pr_ordinal_idx",
	"228_channel_media_pending_object_key_index":                "channel_media_pending_object_storage_key_uidx",
	"230_channel_media_pending_object_claim_index":              "idx_channel_media_pending_object_claim",
	"231_agent_task_queue_terminal_completed_at_index":          "idx_agent_task_queue_terminal_completed_at",
	"232_channel_media_pending_object_due_index":                "idx_channel_media_pending_object_due",
	"233_agent_task_queue_agent_terminal_latest_index":          "idx_agent_task_queue_agent_terminal_latest",
	"238_quick_action_workspace_index":                          "idx_quick_action_workspace_status_usage",
	"241_comment_parent_lookup_index":                           "idx_comment_workspace_issue_parent",
	"244_issue_dependency_issue_index":                          "idx_issue_dependency_issue_id",
	"245_issue_dependency_depends_on_index":                     "idx_issue_dependency_depends_on_issue_id",
	"246_inbox_item_issue_index":                                "idx_inbox_item_issue_id",
	"247_comment_parent_index":                                  "idx_comment_parent_id",
	"248_agent_task_trigger_comment_index":                      "idx_agent_task_queue_trigger_comment_id",
	"255_agent_task_queue_chat_pending_deferred_v3":             "idx_agent_task_queue_chat_pending_v3",
	"257_agent_task_queue_channel_media_pending_unique_v2":      "idx_one_pending_task_per_issue_agent_v2",
	"261_agent_task_queue_terminal_completed_at_v2":             "idx_agent_task_queue_terminal_completed_at_v2",
	"266_issue_view_owner_index":                                "idx_issue_view_owner",
	"267_issue_view_shared_index":                               "idx_issue_view_shared",
	"273_agent_task_queue_runtime_id_index":                     "idx_agent_task_queue_runtime_id",
	"274_task_token_workspace_id_index":                         "idx_task_token_workspace_id",
	"275_task_token_agent_id_index":                             "idx_task_token_agent_id",
	"276_chat_draft_restore_task_id_index":                      "idx_chat_draft_restore_task_id",
	"277_autopilot_run_task_id_index":                           "idx_autopilot_run_task_id",
	"278_agent_task_queue_agent_id_keyset_index":                "idx_agent_task_queue_agent_id_keyset",
	"279_agent_task_queue_issue_id_keyset_index":                "idx_agent_task_queue_issue_id_keyset",
	"281_agent_workspace_id_keyset_index":                       "idx_agent_workspace_id_keyset",
	"282_issue_workspace_id_keyset_index":                       "idx_issue_workspace_id_keyset",
	"283_agent_runtime_workspace_id_keyset_index":               "idx_agent_runtime_workspace_id_keyset",
	"286_plugin_identity_key_index":                             "idx_plugin_identity_key",
	"287_plugin_release_version_index":                          "idx_plugin_release_version",
	"288_plugin_installation_workspace_plugin_index":            "idx_plugin_installation_workspace_plugin_active",
	"289_plugin_contribution_key_index":                         "idx_plugin_contribution_release_key",
	"290_plugin_contribution_ordinal_index":                     "idx_plugin_contribution_release_ordinal",
	"291_plugin_grant_revision_index":                           "idx_plugin_grant_revision",
	"292_plugin_binding_revision_index":                         "idx_plugin_binding_revision",
	"293_plugin_installation_workspace_index":                   "idx_plugin_installation_workspace",
	"295_plugin_artifact_file_index":                            "idx_plugin_artifact_file_release_path",
	"296_plugin_snapshot_revision_index":                        "idx_plugin_snapshot_workspace_revision",
	"297_plugin_execution_task_index":                           "idx_plugin_execution_manifest_task",
	"298_plugin_health_index":                                   "idx_plugin_health_installation_observed",
	"299_agent_task_plugin_manifest_index":                      "idx_agent_task_plugin_execution_manifest",
	"305_dingtalk_group_route_installation_conversation_unique": "idx_dingtalk_group_route_installation_conversation",
	"306_dingtalk_group_route_workspace_index":                  "idx_dingtalk_group_route_workspace",
	"307_dingtalk_group_route_id_unique":                        "idx_dingtalk_group_route_id_unique",
	"309_agent_runtime_id_index":                                "idx_agent_runtime_id",
	"311_plugin_identity_scoped_key_index":                      "idx_plugin_identity_scoped_key",
	"316_workspace_mcp_server_name_unique":                      "idx_workspace_mcp_server_workspace_name",
	"317_agent_mcp_server_server_index":                         "idx_agent_mcp_server_server",
	"320_plugin_installation_config_revision_index":             "idx_plugin_installation_config_contribution_revision",
	"321_plugin_installation_config_workspace_index":            "idx_plugin_installation_config_workspace",
	"322_plugin_remote_mcp_secret_revision_index":               "idx_plugin_remote_mcp_secret_revision",
	"323_plugin_remote_mcp_secret_workspace_index":              "idx_plugin_remote_mcp_secret_workspace",
	"324_plugin_remote_mcp_one_active_secret_index":             "idx_plugin_remote_mcp_one_active_secret",
	"326_plugin_remote_mcp_oauth_state_expiry_index":            "plugin_remote_mcp_oauth_state_expiry_idx",
	"328_workspace_share_link_id_uidx":                          "workspace_share_link_pkey_uidx",
	"330_workspace_share_link_active_ws_uidx":                   "workspace_share_link_active_ws_uidx",
	"331_workspace_share_link_code_uidx":                        "workspace_share_link_code_uidx",
	"333_issue_status_pkey_index":                               "issue_status_pkey_uidx",
	"335_issue_status_workspace_key_index":                      "idx_issue_status_workspace_key",
	"336_issue_status_workspace_name_index":                     "idx_issue_status_workspace_name_active",
	"343_comment_delegated_failure_pending_index":               "idx_comment_delegated_failure_pending",
	"345_plugin_installation_workspace_key_index":               "idx_plugin_installation_workspace_key",
	"346_plugin_storage_scope_key_index":                        "idx_plugin_storage_scope_key",
	"347_plugin_secret_installation_key_index":                  "idx_plugin_secret_installation_key",
	"349_agent_task_queue_chat_terminal_resume_index":           "idx_agent_task_queue_chat_terminal_resume",
	"350_agent_task_queue_chat_retired_session_index":           "idx_agent_task_queue_chat_retired_session",
	"353_autopilot_quota_period_scope_index":                    "uq_autopilot_quota_period_scope",
	"354_autopilot_quota_reservation_id_index":                  "autopilot_quota_reservation_pkey_uidx",
	"355_autopilot_quota_reservation_key_index":                 "uq_autopilot_quota_reservation_key",
	"356_autopilot_run_quota_reservation_index":                 "uq_autopilot_run_quota_reservation",
	"357_webhook_delivery_replay_idempotency_index":             "uq_webhook_delivery_replay_idempotency",
	"358_autopilot_quota_reservation_state_index":               "idx_autopilot_quota_reservation_state",
	"361_issue_last_activity_index":                             "idx_issue_workspace_last_activity",
}

// concurrentDownIndexCleanups covers every migration whose down direction
// rebuilds an index with CREATE INDEX CONCURRENTLY. An interrupted rollback
// can leave an INVALID relation behind. IF NOT EXISTS would then silently skip
// the retry, while a bare CREATE would stay wedged on "already exists"; both
// cases need direction-specific cleanup before the rollback can retry safely.
var concurrentDownIndexCleanups = map[string]string{
	"144_drop_agent_task_queue_chat_pending_v1":             "idx_agent_task_queue_chat_pending",
	"171_drop_legacy_label_namespace_index":                 "issue_label_workspace_name_lower_idx",
	"256_drop_agent_task_queue_chat_pending_v2":             "idx_agent_task_queue_chat_pending_v2",
	"258_drop_pending_issue_agent_v1":                       "idx_one_pending_task_per_issue_agent",
	"262_drop_agent_task_queue_terminal_completed_at_v1":    "idx_agent_task_queue_terminal_completed_at",
	"300_drop_redundant_issue_workspace_number_index":       "idx_issue_workspace_number",
	"301_drop_redundant_sys_cron_job_plan_index":            "idx_sys_cron_exec_job_plan",
	"302_drop_redundant_channel_chat_session_binding_index": "idx_channel_chat_session_binding_session",
	"303_drop_redundant_lark_chat_session_binding_index":    "idx_lark_chat_session_binding_session",
	"312_drop_global_plugin_identity_key_index":             "idx_plugin_identity_key",
}

var preMigrationHooks = func() map[string]preMigrationHook {
	hooks := map[string]preMigrationHook{
		"103_drop_legacy_daily_rollups":                         runTaskUsageHourlyHook,
		"198_agent_task_attribution_strict_constraint_validate": runAttributionStrictHook,
	}
	for version, index := range concurrentIndexCleanups {
		hooks[version] = cleanupInvalidConcurrentIndexHook(index)
	}
	return hooks
}()

var preRollbackHooks = func() map[string]preMigrationHook {
	hooks := make(map[string]preMigrationHook, len(concurrentDownIndexCleanups))
	for version, index := range concurrentDownIndexCleanups {
		hooks[version] = cleanupInvalidConcurrentIndexHook(index)
	}
	return hooks
}()

func hooksForDirection(direction string) map[string]preMigrationHook {
	switch direction {
	case "up":
		return preMigrationHooks
	case "down":
		return preRollbackHooks
	default:
		return nil
	}
}

// cleanupInvalidConcurrentIndexHook removes an INVALID index left by an
// interrupted or failed CREATE INDEX CONCURRENTLY before the migration retries.
// Without this guard, CREATE INDEX ... IF NOT EXISTS would treat the leftover
// relation as success and allow a later migration to drop the still-valid old
// index. Non-index relations fail closed instead of being dropped implicitly.
func cleanupInvalidConcurrentIndexHook(indexRegclass string) preMigrationHook {
	return func(ctx context.Context, pool *pgxpool.Pool) error {
		var schemaName, relationName string
		var isIndex, isValid bool
		err := pool.QueryRow(ctx, `
			SELECT n.nspname, c.relname, c.relkind = 'i', COALESCE(i.indisvalid, FALSE)
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_index i ON i.indexrelid = c.oid
			WHERE c.oid = to_regclass($1)
		`, indexRegclass).Scan(&schemaName, &relationName, &isIndex, &isValid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect concurrent index %q: %w", indexRegclass, err)
		}
		if !isIndex {
			return fmt.Errorf("relation %q exists but is not an index", indexRegclass)
		}
		if isValid {
			return nil
		}

		qualifiedName := pgx.Identifier{schemaName, relationName}.Sanitize()
		if _, err := pool.Exec(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+qualifiedName); err != nil {
			return fmt.Errorf("drop invalid concurrent index %s: %w", qualifiedName, err)
		}
		slog.Warn("removed invalid index before migration retry", "index", qualifiedName)
		return nil
	}
}

func runTaskUsageHourlyHook(ctx context.Context, pool *pgxpool.Pool) error {
	res, err := taskusagebackfill.Hook(ctx, pool, taskusagebackfill.HookOptions{})
	if err != nil {
		return fmt.Errorf("task_usage_hourly pre-103 hook: %w", err)
	}
	if res.Skipped != "" {
		slog.Info("task_usage hourly rollup hook: skipped",
			"reason", res.Skipped,
			"watermark_stamped", res.WatermarkStamped)
		return nil
	}
	slog.Info("task_usage hourly rollup hook: backfill complete",
		"slices", res.SlicesProcessed,
		"rows_touched", res.RowsTouched,
		"from", res.From.Format("2006-01-02T15:04:05Z07:00"),
		"to", res.To.Format("2006-01-02T15:04:05Z07:00"))
	return nil
}

// runAttributionStrictHook backfills accountable_user_id from
// originator_user_id before migration 198 validates the strict attribution
// constraint, so self-hosted upgrades that never ran the out-of-band
// backfill recover automatically (GH #5544 / MUL-4897).
func runAttributionStrictHook(ctx context.Context, pool *pgxpool.Pool) error {
	res, err := attributionbackfill.Hook(ctx, pool, attributionbackfill.HookOptions{})
	if err != nil {
		return fmt.Errorf("attribution strict-constraint pre-198 hook: %w", err)
	}
	slog.Info("attribution backfill hook: complete",
		"rows_backfilled", res.RowsBackfilled,
		"batches", res.Batches,
		"mismatch_normalized", res.MismatchNormalized)
	return nil
}

// migrationAdvisoryLockKey is the int64 identifier used with Postgres
// pg_advisory_lock to serialize the migration loop across concurrent
// runners (multi-replica backend Deployment, scale-up, or a manual
// `migrate up` overlapping with pod startup). The exact value is
// arbitrary — it just needs to be stable across every process that runs
// migrations against the same database. See GitHub multica-ai/multica#3647.
const migrationAdvisoryLockKey int64 = 7244554146635925501

// defaultSchemaMigrationsTable is the unqualified name of the bookkeeping
// table that tracks which migrations have been applied. Tests override
// this so a concurrent-race harness can run against the same shared
// Postgres without colliding with the production table.
const defaultSchemaMigrationsTable = "schema_migrations"

// runOptions carries everything runMigrations needs that is not the
// pool itself. Tests use it to inject a hermetic migrations directory,
// a unique per-test bookkeeping table, and a unique advisory-lock key
// that doesn't collide with any other migration runner sharing the same
// Postgres instance.
type runOptions struct {
	// Direction is "up" or "down".
	Direction string
	// Files is the ordered list of .sql files to apply. Production callers
	// pass migrations.Files(direction); tests pass a curated set written
	// to a t.TempDir().
	Files []string
	// SchemaMigrationsTable is the bookkeeping table to read/write.
	// May be schema-qualified (e.g. "migrate_test_xyz.schema_migrations").
	// Empty means defaultSchemaMigrationsTable.
	SchemaMigrationsTable string
	// AdvisoryLockKey is the int64 used with pg_advisory_lock. Zero means
	// migrationAdvisoryLockKey. Tests pass a unique key per run so
	// concurrent test workers do not block on the production migration
	// runner if it happens to share the database.
	AdvisoryLockKey int64
	// Hooks maps migration version → pre-migration hook. The hook
	// receives the pool (not the loop's pinned conn) so it can take
	// its own session-level locks. nil or missing entries mean "no
	// hook" and the migration runs straight through. Production main()
	// passes the direction-specific hook map; tests leave this nil unless they
	// exercise a hook.
	Hooks map[string]preMigrationHook
}

func main() {
	logger.Init()

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run ./cmd/migrate <up|down>")
		os.Exit(1)
	}

	direction := os.Args[1]
	if direction != "up" && direction != "down" {
		fmt.Println("Usage: go run ./cmd/migrate <up|down>")
		os.Exit(1)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}

	files, err := migrations.Files(direction)
	if err != nil {
		slog.Error("failed to find migration files", "error", err)
		os.Exit(1)
	}

	if err := runMigrations(ctx, pool, runOptions{
		Direction: direction,
		Files:     files,
		Hooks:     hooksForDirection(direction),
	}); err != nil {
		slog.Error("migration run failed", "error", err)
		os.Exit(1)
	}

	fmt.Println("Done.")
}

// runMigrations applies (direction="up") or rolls back (direction="down")
// the given file list against the supplied pool, serialized through a
// Postgres session-level advisory lock so multiple concurrent runners
// (multi-replica startup, scale-up, manual migrate overlap) take turns
// instead of racing each other.
//
// It is safe to invoke concurrently from multiple goroutines or
// processes against the same database with the same options: every
// caller blocks on pg_advisory_lock, and once it is their turn the
// already-applied EXISTS check turns each finished migration into a
// no-op skip. See GitHub multica-ai/multica#3647 / MUL-2923.
func runMigrations(ctx context.Context, pool *pgxpool.Pool, opts runOptions) error {
	switch opts.Direction {
	case "up", "down":
		// ok
	default:
		return fmt.Errorf("invalid direction %q (want \"up\" or \"down\")", opts.Direction)
	}

	table := opts.SchemaMigrationsTable
	if table == "" {
		table = defaultSchemaMigrationsTable
	}
	tableIdent, err := quoteQualifiedIdentifier(table)
	if err != nil {
		return fmt.Errorf("invalid schema migrations table %q: %w", table, err)
	}
	lockKey := opts.AdvisoryLockKey
	if lockKey == 0 {
		lockKey = migrationAdvisoryLockKey
	}

	// pg_advisory_lock is scoped to a single session, so we must pin one
	// *pgxpool.Conn for the whole run — calling pool.Exec would attach the
	// lock to a random connection that pgxpool could hand back out before
	// the loop finishes, making the lock effectively a no-op. We use the
	// blocking pg_advisory_lock (not pg_try_*) so a late-arriving runner
	// queues behind the current one instead of crash-looping; once it
	// acquires the lock the EXISTS checks below turn finished migrations
	// into no-op skips.
	//
	// We deliberately do NOT wrap the loop in a single transaction: the
	// repo already ships migrations using CREATE INDEX CONCURRENTLY,
	// which Postgres rejects inside a transaction block.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	// Best-effort explicit unlock on the success path. On error returns
	// the defer still runs; on os.Exit error paths in main() it does not,
	// but session-level advisory locks are released automatically when
	// the connection closes at process exit, so the next runner is never
	// permanently blocked.
	defer func() {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			slog.Warn("failed to release migration advisory lock", "error", err)
		}
	}()

	// Create migrations tracking table.
	if _, err := conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`, tableIdent)); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	existsSQL := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE version = $1)", tableIdent)
	insertSQL := fmt.Sprintf("INSERT INTO %s (version) VALUES ($1)", tableIdent)
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE version = $1", tableIdent)

	for _, file := range opts.Files {
		version := migrations.ExtractVersion(file)

		var exists bool
		if err := conn.QueryRow(ctx, existsSQL, version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %q: %w", version, err)
		}

		if opts.Direction == "up" {
			if exists {
				fmt.Printf("  skip  %s (already applied)\n", version)
				continue
			}
		} else {
			if !exists {
				fmt.Printf("  skip  %s (not applied)\n", version)
				continue
			}
		}

		sql, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", file, err)
		}

		// Run any pre-migration hook before the SQL file. Hooks
		// receive the *pgxpool.Pool (not the loop's pinned conn), so
		// they can acquire other session-level locks without
		// colliding with migrationAdvisoryLockKey. Hook failures
		// abort the run before schema_migrations is updated, so the
		// same version retries cleanly on the next invocation.
		if hook, ok := opts.Hooks[version]; ok && hook != nil {
			slog.Info("running pre-migration hook", "version", version, "direction", opts.Direction)
			if err := hook(ctx, pool); err != nil {
				return fmt.Errorf("pre-migration hook for %q (%s): %w", version, opts.Direction, err)
			}
		}

		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply migration %q: %w", file, err)
		}

		if opts.Direction == "up" {
			_, err = conn.Exec(ctx, insertSQL, version)
		} else {
			_, err = conn.Exec(ctx, deleteSQL, version)
		}
		if err != nil {
			return fmt.Errorf("record migration %q: %w", version, err)
		}

		fmt.Printf("  %s  %s\n", opts.Direction, version)
	}

	return nil
}

// quoteQualifiedIdentifier safely quotes either an unqualified table
// name ("foo") or a schema-qualified name ("schema.foo") for embedding
// into a SQL statement. Postgres does not let parametrized queries
// supply identifiers, so we have to interpolate, but pgx.Identifier
// does the right escaping (double-quotes, embedded-quote handling).
//
// The accepted shape is exactly one or two dot-separated components.
// Names containing more than one dot are rejected outright rather than
// silently sanitized into a "schema"."b.c" reference, which is valid
// SQL but almost certainly not what the caller meant.
func quoteQualifiedIdentifier(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty identifier")
	}
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return "", fmt.Errorf("identifier %q has more than one dot; only schema.table is supported", name)
	}
	for _, p := range parts {
		if p == "" {
			return "", fmt.Errorf("empty component in %q", name)
		}
	}
	return pgx.Identifier(parts).Sanitize(), nil
}
