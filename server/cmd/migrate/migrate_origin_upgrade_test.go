package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/migrations"
)

// historicalShareCRMOriginVersions match origin_compat_lint_test.go. They are
// repeated here because that list lives in the migrations package's tests.
var historicalShareCRMOriginVersions = []string{
	"649_issue_origin_sharecrm_chat",
	"650_issue_origin_sharecrm_chat_validate",
	"657_issue_origin_sharecrm_chat",
	"658_issue_origin_sharecrm_chat_validate",
	"661_issue_origin_sharecrm_chat",
	"662_issue_origin_sharecrm_chat_validate",
	"665_issue_origin_sharecrm_chat",
	"666_issue_origin_sharecrm_chat_validate",
	"673_issue_origin_sharecrm_chat",
	"674_issue_origin_sharecrm_chat_validate",
	"681_issue_origin_sharecrm_chat",
	"682_issue_origin_sharecrm_chat_validate",
	"683_issue_origin_sharecrm_chat",
	"684_issue_origin_sharecrm_chat_validate",
	"685_issue_origin_sharecrm_chat",
	"686_issue_origin_sharecrm_chat_validate",
	"695_issue_origin_sharecrm_chat",
	"696_issue_origin_sharecrm_chat_validate",
	"697_issue_origin_sharecrm_chat",
	"698_issue_origin_sharecrm_chat_validate",
	"703_issue_origin_sharecrm_chat",
	"704_issue_origin_sharecrm_chat_validate",
	"711_issue_origin_sharecrm_chat",
	"712_issue_origin_sharecrm_chat_validate",
	"732_issue_origin_sharecrm_chat",
	"733_issue_origin_sharecrm_chat_validate",
	"735_issue_origin_sharecrm_chat",
	"736_issue_origin_sharecrm_chat_validate",
	"743_issue_origin_sharecrm_chat",
	"744_issue_origin_sharecrm_chat_validate",
	"754_issue_origin_sharecrm_chat",
	"755_issue_origin_sharecrm_chat_validate",
}

// migrationsAfter742 are upstream 743-755 plus the ShareCRM pair this release
// ships. Recorded-key upgrades skip them while building the previous schema,
// so the upgrade step is what applies them. A database that already recorded
// 743_issue_origin_sharecrm_chat still runs 743_issue_blocker, and one that
// recorded 754_issue_origin_sharecrm_chat still runs
// 754_issue_stage_blockers_skip_top_level: the ledger key is the whole filename.
var migrationsAfter742 = []string{
	"743_issue_blocker",
	"744_issue_blocker_unique",
	"745_issue_blocker_reverse",
	"746_issue_blocker_rules",
	"747_squad_handoff_mode",
	"748_app_tool_call_quota_period",
	"749_app_tool_call_quota_period_scope_idx",
	"750_app_tool_call_quota_columns",
	"751_app_tool_call_workspace_metered_idx",
	"752_doc_comment_agent_runs",
	"753_agent_task_queue_doc_idx",
	"754_issue_stage_blockers_skip_top_level",
	"755_task_usage_hourly_rollup_hour_key",
	"756_issue_origin_sharecrm_chat",
	"757_issue_origin_sharecrm_chat_validate",
}

// migrationsAfter753 are the files this sync adds on top of the previous
// ShareCRM tip (upstream through 753, ShareCRM recorded as 754/755).
var migrationsAfter753 = []string{
	"754_issue_stage_blockers_skip_top_level",
	"755_task_usage_hourly_rollup_hour_key",
	"756_issue_origin_sharecrm_chat",
	"757_issue_origin_sharecrm_chat_validate",
}

func TestIssueOriginUpgradeLedgerPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("postgres upgrade regression")
	}
	cases := []struct {
		name    string
		ledger  []string
		prepare func(t *testing.T, ctx context.Context, pool *pgxpool.Pool)
	}{
		{
			name:   "deployed_665_666",
			ledger: []string{"665_issue_origin_sharecrm_chat", "666_issue_origin_sharecrm_chat_validate"},
			prepare: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				prepareShareCRMIssue(t, ctx, pool, true)
			},
		},
		{
			name:   "historical_649_650_only",
			ledger: []string{"649_issue_origin_sharecrm_chat", "650_issue_origin_sharecrm_chat_validate"},
			prepare: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				prepareShareCRMIssue(t, ctx, pool, true)
			},
		},
		{
			name:   "every_historical_filename",
			ledger: historicalShareCRMOriginVersions,
			prepare: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				prepareShareCRMIssue(t, ctx, pool, true)
			},
		},
		{
			name:   "widen_recorded_validate_not",
			ledger: []string{"661_issue_origin_sharecrm_chat"},
			prepare: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				prepareShareCRMIssue(t, ctx, pool, false)
			},
		},
		{
			name: "upstream_weixin_already_validated",
			ledger: []string{
				"667_issue_origin_weixin_chat",
				"668_issue_origin_weixin_chat_validate",
			},
			prepare: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				prepareWeixinOnlyIssue(t, ctx, pool)
			},
		},
		{
			name: "shipped_673_674_sharecrm",
			ledger: []string{
				"673_issue_origin_sharecrm_chat",
				"674_issue_origin_sharecrm_chat_validate",
			},
			prepare: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
				prepareShareCRMIssue(t, ctx, pool, true)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, pool, schema := newOriginSandbox(t)
			tc.prepare(t, ctx, pool)
			insertLedger(t, ctx, pool, tc.ledger)
			before := snapshotOrigins(t, ctx, pool)
			runOriginMigrations(t, ctx, pool, schema)
			assertOriginsKept(t, ctx, pool, before)
			assertNewChannelWrites(t, ctx, pool)
			for _, version := range tc.ledger {
				assertLedger(t, ctx, pool, version, true)
			}
			oid := originConstraintOID(t, ctx, pool)
			runOriginMigrations(t, ctx, pool, schema)
			if got := originConstraintOID(t, ctx, pool); got != oid {
				t.Fatalf("second migrate rebuilt the origin check: oid %d -> %d", oid, got)
			}
			assertOriginsKept(t, ctx, pool, before)
			assertValidatedChannelOrigins(t, ctx, pool)
		})
	}
}

func TestStuckAtUpstream667RecoversOnNextMigrate(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, true)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat'
			))
			NOT VALID
	`); err != nil {
		t.Fatal(err)
	}
	var rejected bool
	if _, err := pool.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ('00000000-0000-4000-8000-000000000099', 'sharecrm_chat')`); err == nil {
		t.Fatal("sharecrm_chat insert succeeded under the already-committed weixin-only check")
	} else {
		rejected = true
	}
	if !rejected {
		t.Fatal("expected the stuck check to reject sharecrm_chat")
	}
	before := snapshotOrigins(t, ctx, pool)
	insertLedger(t, ctx, pool, append(append([]string{}, historicalShareCRMOriginVersions...), "667_issue_origin_weixin_chat"))

	runOriginMigrations(t, ctx, pool, schema)
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertLedger(t, ctx, pool, "667_issue_origin_weixin_chat", true)
	assertLedger(t, ctx, pool, "668_issue_origin_weixin_chat_validate", true)
	for _, version := range historicalShareCRMOriginVersions {
		assertLedger(t, ctx, pool, version, true)
	}
	if !originListContains(t, ctx, pool, "custom_origin") || !originListContains(t, ctx, pool, "weird'origin") {
		t.Fatal("recovery dropped an origin that still had rows")
	}
}

func TestEmptyDatabaseUpgradeAllowsChannelOriginsAndIsIdempotent(t *testing.T) {
	admin := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	name := fmt.Sprintf("origin_empty_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		_, _ = admin.Exec(dropCtx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = admin.Exec(dropCtx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize())
	})

	pool := openPoolURL(t, databaseURLFor(t, name))
	files, err := migrations.Files("up")
	if err != nil {
		t.Fatal(err)
	}
	options := runOptions{
		Direction:             "up",
		Files:                 files,
		SchemaMigrationsTable: "schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
		Conditions:            conditionsForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("empty database upgrade: %v", err)
	}
	assertValidatedChannelOrigins(t, ctx, pool)
	insertChannelIssue(t, ctx, pool, "sharecrm_chat")
	insertChannelIssue(t, ctx, pool, "weixin_chat")
	assertLedger(t, ctx, pool, "673_runtime_rate_limit", true)
	assertLedger(t, ctx, pool, "665_runtime_rate_limit", false)
	assertLedger(t, ctx, pool, "675_runtime_rate_limit", false)
	assertLedger(t, ctx, pool, "681_project_folder", true)
	assertLedger(t, ctx, pool, "682_project_folder_name_idx", true)
	assertLedger(t, ctx, pool, "683_entitlement_feature_attempt", true)
	assertLedger(t, ctx, pool, "684_entitlement_feature_attempt_scope_idx", true)
	assertLedger(t, ctx, pool, "697_autopilot_run_idempotency_key", true)
	assertLedger(t, ctx, pool, "698_autopilot_run_idempotency_key_idx", true)
	assertLedger(t, ctx, pool, "699_member_guest_role", true)
	assertLedger(t, ctx, pool, "700_member_search_access", true)
	assertLedger(t, ctx, pool, "701_member_search_access_pkey_index", true)
	assertLedger(t, ctx, pool, "702_member_search_access_pkey", true)
	assertLedger(t, ctx, pool, "703_doc_comment", true)
	assertLedger(t, ctx, pool, "704_doc_comment_pkey_index", true)
	assertLedger(t, ctx, pool, "705_doc_comment_pkey", true)
	assertLedger(t, ctx, pool, "706_doc_comment_doc_idx", true)
	assertLedger(t, ctx, pool, "707_doc_comment_workspace_idx", true)
	assertLedger(t, ctx, pool, "708_attachment_workspace_size_idx", true)
	assertLedger(t, ctx, pool, "709_drop_attachment_workspace_index", true)
	assertLedger(t, ctx, pool, "710_autopilot_quota_limit_notice", true)
	for _, version := range migrationsAfter742 {
		assertLedger(t, ctx, pool, version, true)
	}
	assertLedger(t, ctx, pool, "743_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "744_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "754_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "755_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "735_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "736_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "735_task_usage_hourly_squad_col", true)
	assertLedger(t, ctx, pool, "736_task_usage_hourly_squad_uq_idx", true)
	assertLedger(t, ctx, pool, "737_task_usage_hourly_dirty_squad_uq_idx", true)
	assertLedger(t, ctx, pool, "738_task_usage_hourly_workspace_squad_idx", true)
	assertLedger(t, ctx, pool, "739_task_usage_hourly_squad_pipeline", true)
	assertLedger(t, ctx, pool, "740_task_usage_hourly_drop_legacy_key", true)
	assertLedger(t, ctx, pool, "741_task_usage_hourly_dirty_drop_legacy_key", true)
	assertLedger(t, ctx, pool, "742_task_usage_hourly_squad_rollback_merge", true)
	assertLedger(t, ctx, pool, "732_comment_idempotency_receipt", true)
	assertLedger(t, ctx, pool, "733_comment_idempotency_receipt_key", true)
	assertLedger(t, ctx, pool, "734_comment_idempotency_receipt_created", true)
	assertLedger(t, ctx, pool, "732_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "733_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "703_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "704_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "697_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "698_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "695_inbox_item_returned_at", true)
	assertLedger(t, ctx, pool, "696_inbox_item_active_shown_idx", true)
	assertLedger(t, ctx, pool, "695_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "696_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "685_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "686_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "683_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "684_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "681_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "682_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "665_weixin_state", true)
	assertLedger(t, ctx, pool, "685_docs", true)
	assertLedger(t, ctx, pool, "686_doc_folder_pkey_index", true)
	assertLedger(t, ctx, pool, "689_doc_pkey", true)
	assertLedger(t, ctx, pool, "694_doc_version_workspace_idx", true)
	assertIndexValid(t, ctx, pool, "idx_inbox_active_shown")
	assertIndexValid(t, ctx, pool, "uq_autopilot_run_idempotency_key")
	assertRelation(t, ctx, pool, "member_search_access")
	assertIndexValid(t, ctx, pool, "member_search_access_pkey")
	var hasReturnedAt bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'inbox_item'
			  AND column_name = 'returned_at'
		)
	`).Scan(&hasReturnedAt); err != nil || !hasReturnedAt {
		t.Fatalf("inbox_item.returned_at present = %t, error = %v", hasReturnedAt, err)
	}
	assertRelation(t, ctx, pool, "runtime_rate_limit")
	assertRelation(t, ctx, pool, "doc")
	assertRelation(t, ctx, pool, "doc_folder")
	assertRelation(t, ctx, pool, "doc_version")
	assertIndexValid(t, ctx, pool, "doc_pkey")
	assertIndexValid(t, ctx, pool, "doc_version_workspace_idx")
	assertRelation(t, ctx, pool, "doc_comment")
	assertIndexValid(t, ctx, pool, "doc_comment_pkey")
	assertIndexValid(t, ctx, pool, "doc_comment_doc_idx")
	assertIndexValid(t, ctx, pool, "doc_comment_workspace_idx")
	assertIndexValid(t, ctx, pool, "idx_attachment_workspace_size")
	assertIndexAbsent(t, ctx, pool, "idx_attachment_workspace")
	var hasNotifiedLimit bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'autopilot_quota_period'
			  AND column_name = 'rejection_notified_limit'
		)
	`).Scan(&hasNotifiedLimit); err != nil || !hasNotifiedLimit {
		t.Fatalf("autopilot_quota_period.rejection_notified_limit present = %t, error = %v", hasNotifiedLimit, err)
	}
	assertRelation(t, ctx, pool, "weixin_login_session")
	assertRelation(t, ctx, pool, "project_folder")
	assertRelation(t, ctx, pool, "entitlement_feature_attempt")
	assertIndexValid(t, ctx, pool, "uq_entitlement_feature_attempt_scope")
	assertRelation(t, ctx, pool, "comment_idempotency_receipt")
	assertIndexValid(t, ctx, pool, "uq_comment_idempotency_receipt_key")
	assertIndexValid(t, ctx, pool, "idx_comment_idempotency_receipt_created")
	assertSquadUsageShape(t, ctx, pool)
	assertBlockerAndQuotaShape(t, ctx, pool)
	assertStageBlockerSkipAndUsageRollup(t, ctx, pool)

	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	oid := originConstraintOID(t, ctx, pool)
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("second migrate recorded %d versions, had %d", after, before)
	}
	if got := originConstraintOID(t, ctx, pool); got != oid {
		t.Fatalf("second migrate rebuilt the origin check: oid %d -> %d", oid, got)
	}
	assertValidatedChannelOrigins(t, ctx, pool)
	assertUniqueLedgerPrefixes(t, ctx, pool)
	var sharecrmRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE origin_type = 'sharecrm_chat'`).Scan(&sharecrmRows); err != nil || sharecrmRows != 1 {
		t.Fatalf("sharecrm_chat rows = %d, error = %v", sharecrmRows, err)
	}
}

func TestCurrentShareCRMReleaseUpgradesWithoutRebuildingRateLimit(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, true)
	if _, err := pool.Exec(ctx, `
		CREATE TABLE runtime_rate_limit (
			runtime_id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			snapshot JSONB NOT NULL,
			observed_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		INSERT INTO runtime_rate_limit (runtime_id, workspace_id, snapshot, observed_at)
		VALUES ('00000000-0000-4000-8000-000000000041', '00000000-0000-4000-8000-000000000042', '{"kept":true}', now())
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, append(append([]string{}, historicalShareCRMOriginVersions...),
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
		"675_runtime_rate_limit",
	))
	var originalOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&originalOID); err != nil {
		t.Fatal(err)
	}
	before := snapshotOrigins(t, ctx, pool)
	files := realMigrationFiles(t, []string{
		"673_runtime_rate_limit",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
	}, "up")
	options := runOptions{
		Direction: "up", Files: files,
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("second up: %v", err)
	}
	var currentOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&currentOID); err != nil {
		t.Fatal(err)
	}
	if currentOID != originalOID {
		t.Fatal("upgrade rebuilt runtime_rate_limit already recorded as 675_runtime_rate_limit")
	}
	var snapshots int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_rate_limit`).Scan(&snapshots); err != nil || snapshots != 1 {
		t.Fatalf("snapshots = %d, error = %v", snapshots, err)
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertLedger(t, ctx, pool, "675_runtime_rate_limit", true)
	assertLedger(t, ctx, pool, "673_runtime_rate_limit", true)
	assertLedger(t, ctx, pool, "673_issue_origin_sharecrm_chat", true)
	assertLedger(t, ctx, pool, "674_issue_origin_sharecrm_chat_validate", true)
	assertLedger(t, ctx, pool, "681_issue_origin_sharecrm_chat", true)
	assertLedger(t, ctx, pool, "682_issue_origin_sharecrm_chat_validate", true)
	assertLedger(t, ctx, pool, "683_issue_origin_sharecrm_chat", true)
	assertLedger(t, ctx, pool, "684_issue_origin_sharecrm_chat_validate", true)
	assertLedger(t, ctx, pool, "703_issue_origin_sharecrm_chat", true)
	assertLedger(t, ctx, pool, "704_issue_origin_sharecrm_chat_validate", true)
	assertLedger(t, ctx, pool, "743_issue_origin_sharecrm_chat", true)
	assertLedger(t, ctx, pool, "744_issue_origin_sharecrm_chat_validate", true)
	assertLedger(t, ctx, pool, "756_issue_origin_sharecrm_chat", true)
	assertLedger(t, ctx, pool, "757_issue_origin_sharecrm_chat_validate", true)
}

func TestPreSyncShareCRMSchemaUpgradesThroughProjectFolders(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, true)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			));
		CREATE TABLE project (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL
		);
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, append(append([]string{}, historicalShareCRMOriginVersions...),
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
	))
	before := snapshotOrigins(t, ctx, pool)
	oid := originConstraintOID(t, ctx, pool)
	files := realMigrationFiles(t, []string{
		"681_project_folder",
		"682_project_folder_name_idx",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
	}, "up")
	options := runOptions{
		Direction: "up", Files: files,
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("second up: %v", err)
	}
	if got := originConstraintOID(t, ctx, pool); got != oid {
		t.Fatalf("upgrade rebuilt an origin check that already allowed sharecrm_chat: oid %d -> %d", oid, got)
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertRelation(t, ctx, pool, "project_folder")
	var folderID bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'project'
			  AND column_name = 'folder_id'
		)
	`).Scan(&folderID); err != nil || !folderID {
		t.Fatalf("project.folder_id present = %t, error = %v", folderID, err)
	}
	var indexValid bool
	if err := pool.QueryRow(ctx, `
		SELECT i.indisvalid
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relname = 'uq_project_folder_workspace_name'
	`).Scan(&indexValid); err != nil || !indexValid {
		t.Fatalf("project folder name index valid = %t, error = %v", indexValid, err)
	}
	for _, version := range []string{
		"681_issue_origin_sharecrm_chat",
		"682_issue_origin_sharecrm_chat_validate",
		"681_project_folder",
		"682_project_folder_name_idx",
		"683_issue_origin_sharecrm_chat",
		"684_issue_origin_sharecrm_chat_validate",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
	} {
		assertLedger(t, ctx, pool, version, true)
	}
}

func TestRetiredShareCRMWidenValidatesOnNextMigrate(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, false)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			))
			NOT VALID
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, []string{
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
		"681_issue_origin_sharecrm_chat",
	})
	before := snapshotOrigins(t, ctx, pool)
	if err := runMigrations(ctx, pool, runOptions{
		Direction: "up",
		Files: realMigrationFiles(t, []string{
			"756_issue_origin_sharecrm_chat",
			"757_issue_origin_sharecrm_chat_validate",
		}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
	}); err != nil {
		t.Fatal(err)
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertLedger(t, ctx, pool, "681_issue_origin_sharecrm_chat", true)
	assertLedger(t, ctx, pool, "682_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "683_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "684_issue_origin_sharecrm_chat_validate", false)
	assertLedger(t, ctx, pool, "756_issue_origin_sharecrm_chat", true)
	assertLedger(t, ctx, pool, "757_issue_origin_sharecrm_chat_validate", true)
}

func TestMorningShareCRMSchemaUpgradesThroughEntitlement(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, true)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			));
		CREATE TABLE runtime_rate_limit (
			runtime_id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			snapshot JSONB NOT NULL,
			observed_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		INSERT INTO runtime_rate_limit (runtime_id, workspace_id, snapshot, observed_at)
		VALUES ('00000000-0000-4000-8000-000000000051', '00000000-0000-4000-8000-000000000052', '{"kept":true}', now());
		CREATE TABLE project (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL
		);
		CREATE TABLE project_folder (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			name TEXT NOT NULL
		);
		ALTER TABLE project ADD COLUMN folder_id UUID;
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, append(append([]string{}, historicalShareCRMOriginVersions...),
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
		"675_runtime_rate_limit",
		"681_project_folder",
		"682_project_folder_name_idx",
	))
	var originalRateOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&originalRateOID); err != nil {
		t.Fatal(err)
	}
	before := snapshotOrigins(t, ctx, pool)
	originOID := originConstraintOID(t, ctx, pool)
	files := realMigrationFiles(t, []string{
		"683_entitlement_feature_attempt",
		"684_entitlement_feature_attempt_scope_idx",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
	}, "up")
	options := runOptions{
		Direction: "up", Files: files,
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("second up: %v", err)
	}
	var currentRateOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&currentRateOID); err != nil {
		t.Fatal(err)
	}
	if currentRateOID != originalRateOID {
		t.Fatal("upgrade rebuilt runtime_rate_limit on a database that already had ShareCRM 683/684")
	}
	var snapshots int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_rate_limit`).Scan(&snapshots); err != nil || snapshots != 1 {
		t.Fatalf("snapshots = %d, error = %v", snapshots, err)
	}
	if got := originConstraintOID(t, ctx, pool); got != originOID {
		t.Fatalf("upgrade rebuilt an origin check that already allowed sharecrm_chat: oid %d -> %d", originOID, got)
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertRelation(t, ctx, pool, "entitlement_feature_attempt")
	assertIndexValid(t, ctx, pool, "uq_entitlement_feature_attempt_scope")
	for _, version := range []string{
		"683_issue_origin_sharecrm_chat",
		"684_issue_origin_sharecrm_chat_validate",
		"683_entitlement_feature_attempt",
		"684_entitlement_feature_attempt_scope_idx",
		"685_issue_origin_sharecrm_chat",
		"686_issue_origin_sharecrm_chat_validate",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
	} {
		assertLedger(t, ctx, pool, version, true)
	}
}

// TestShareCRM685686UpgradesThroughDocs is a database already at the previous
// fork tip: ShareCRM origin filenames 685/686 are recorded, the origin check
// is validated, and runtime_rate_limit keeps its OID and row. This merge
// records upstream docs 685-694 and inbox 695/696 under different filenames
// and re-applies the ShareCRM widen as 756/757 without swapping the check.
// Inbox migrations run while sharecrm_chat rows and new writes still succeed.
func TestShareCRM685686UpgradesThroughDocs(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, true)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			));
		CREATE TABLE runtime_rate_limit (
			runtime_id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			snapshot JSONB NOT NULL,
			observed_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		INSERT INTO runtime_rate_limit (runtime_id, workspace_id, snapshot, observed_at)
		VALUES ('00000000-0000-4000-8000-000000000061', '00000000-0000-4000-8000-000000000062', '{"kept":true}', now());
		CREATE TABLE inbox_item (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			recipient_type TEXT NOT NULL,
			recipient_id UUID NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			archived BOOLEAN NOT NULL DEFAULT false
		);
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, append(append([]string{}, historicalShareCRMOriginVersions...),
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
		"673_runtime_rate_limit",
		"683_entitlement_feature_attempt",
		"684_entitlement_feature_attempt_scope_idx",
	))
	var originalRateOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&originalRateOID); err != nil {
		t.Fatal(err)
	}
	before := snapshotOrigins(t, ctx, pool)
	originOID := originConstraintOID(t, ctx, pool)
	files := realMigrationFiles(t, []string{
		"685_docs",
		"686_doc_folder_pkey_index",
		"687_doc_folder_pkey",
		"688_doc_pkey_index",
		"689_doc_pkey",
		"690_doc_version_pkey_index",
		"691_doc_version_pkey",
		"692_doc_folder_workspace_idx",
		"693_doc_workspace_idx",
		"694_doc_version_workspace_idx",
		"695_inbox_item_returned_at",
		"696_inbox_item_active_shown_idx",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
	}, "up")
	options := runOptions{
		Direction: "up", Files: files,
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("second up: %v", err)
	}
	var currentRateOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&currentRateOID); err != nil {
		t.Fatal(err)
	}
	if currentRateOID != originalRateOID {
		t.Fatal("upgrade rebuilt runtime_rate_limit on a database already at ShareCRM 685/686")
	}
	var snapshots int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_rate_limit WHERE snapshot->>'kept' = 'true'`).Scan(&snapshots); err != nil || snapshots != 1 {
		t.Fatalf("kept snapshots = %d, error = %v", snapshots, err)
	}
	if got := originConstraintOID(t, ctx, pool); got != originOID {
		t.Fatalf("upgrade rebuilt an origin check that already allowed sharecrm_chat: oid %d -> %d", originOID, got)
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertRelation(t, ctx, pool, "doc")
	assertRelation(t, ctx, pool, "doc_folder")
	assertRelation(t, ctx, pool, "doc_version")
	assertIndexValid(t, ctx, pool, "doc_pkey")
	assertIndexValid(t, ctx, pool, "doc_version_workspace_idx")
	assertIndexValid(t, ctx, pool, "idx_inbox_active_shown")
	for _, version := range []string{
		"685_issue_origin_sharecrm_chat",
		"686_issue_origin_sharecrm_chat_validate",
		"695_issue_origin_sharecrm_chat",
		"696_issue_origin_sharecrm_chat_validate",
		"685_docs",
		"686_doc_folder_pkey_index",
		"694_doc_version_workspace_idx",
		"695_inbox_item_returned_at",
		"696_inbox_item_active_shown_idx",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
		"673_runtime_rate_limit",
	} {
		assertLedger(t, ctx, pool, version, true)
	}
}

// TestShareCRM695696UpgradesThroughInbox is this morning's fork tip. ShareCRM
// origin filenames 695/696 are already recorded and the origin check is
// validated. Upstream inbox migrations now use 695/696, so they must run
// while existing sharecrm_chat rows and new ShareCRM writes still succeed.
// The renamed tail 756/757 records itself and does not swap that check.
func TestShareCRM695696UpgradesThroughInbox(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, true)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			));
		CREATE TABLE inbox_item (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			recipient_type TEXT NOT NULL,
			recipient_id UUID NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			archived BOOLEAN NOT NULL DEFAULT false
		);
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, append(append([]string{}, historicalShareCRMOriginVersions...),
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
		"685_docs",
		"686_doc_folder_pkey_index",
		"687_doc_folder_pkey",
		"688_doc_pkey_index",
		"689_doc_pkey",
		"690_doc_version_pkey_index",
		"691_doc_version_pkey",
		"692_doc_folder_workspace_idx",
		"693_doc_workspace_idx",
		"694_doc_version_workspace_idx",
	))
	before := snapshotOrigins(t, ctx, pool)
	originOID := originConstraintOID(t, ctx, pool)
	inbox := runOptions{
		Direction: "up",
		Files: realMigrationFiles(t, []string{
			"695_inbox_item_returned_at",
			"696_inbox_item_active_shown_idx",
		}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, inbox); err != nil {
		t.Fatal(err)
	}
	if got := originConstraintOID(t, ctx, pool); got != originOID {
		t.Fatalf("inbox migrations rebuilt the origin check: oid %d -> %d", originOID, got)
	}
	assertOriginsKept(t, ctx, pool, before)
	if _, err := pool.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ('00000000-0000-4000-8000-000000000081', 'sharecrm_chat')`); err != nil {
		t.Fatalf("sharecrm_chat insert after inbox migrations, before 711: %v", err)
	}
	tail := runOptions{
		Direction: "up",
		Files: realMigrationFiles(t, []string{
			"756_issue_origin_sharecrm_chat",
			"757_issue_origin_sharecrm_chat_validate",
		}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
	}
	if err := runMigrations(ctx, pool, tail); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, tail); err != nil {
		t.Fatalf("second tail: %v", err)
	}
	if got := originConstraintOID(t, ctx, pool); got != originOID {
		t.Fatalf("711 rebuilt an origin check that already allowed sharecrm_chat: oid %d -> %d", originOID, got)
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertIndexValid(t, ctx, pool, "idx_inbox_active_shown")
	for _, version := range []string{
		"695_issue_origin_sharecrm_chat",
		"696_issue_origin_sharecrm_chat_validate",
		"695_inbox_item_returned_at",
		"696_inbox_item_active_shown_idx",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
		"685_docs",
		"694_doc_version_workspace_idx",
	} {
		assertLedger(t, ctx, pool, version, true)
	}
}

// TestPureUpstreamValidatedOriginGainsDocsAndShareCRM starts from upstream's
// already-validated weixin-only 667/668. Those filenames stay recorded, so
// the fork's repaired 667 does not run again. Docs 685-694 create the new
// tables, inbox 695/696 add returned_at, and 732 widens the check. An
// existing rate-limit table keeps its OID and row.
func TestPureUpstreamValidatedOriginGainsDocsAndShareCRM(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareWeixinOnlyIssue(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		CREATE TABLE runtime_rate_limit (
			runtime_id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			snapshot JSONB NOT NULL,
			observed_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		INSERT INTO runtime_rate_limit (runtime_id, workspace_id, snapshot, observed_at)
		VALUES ('00000000-0000-4000-8000-000000000071', '00000000-0000-4000-8000-000000000072', '{"kept":true}', now());
		CREATE TABLE inbox_item (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			recipient_type TEXT NOT NULL,
			recipient_id UUID NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			archived BOOLEAN NOT NULL DEFAULT false
		);
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, []string{
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
		"673_runtime_rate_limit",
	})
	var originalRateOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&originalRateOID); err != nil {
		t.Fatal(err)
	}
	before := snapshotOrigins(t, ctx, pool)
	originOID := originConstraintOID(t, ctx, pool)
	options := runOptions{
		Direction: "up",
		Files: realMigrationFiles(t, []string{
			"685_docs",
			"686_doc_folder_pkey_index",
			"687_doc_folder_pkey",
			"688_doc_pkey_index",
			"689_doc_pkey",
			"690_doc_version_pkey_index",
			"691_doc_version_pkey",
			"692_doc_folder_workspace_idx",
			"693_doc_workspace_idx",
			"694_doc_version_workspace_idx",
			"695_inbox_item_returned_at",
			"696_inbox_item_active_shown_idx",
			"756_issue_origin_sharecrm_chat",
			"757_issue_origin_sharecrm_chat_validate",
		}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	var currentRateOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&currentRateOID); err != nil {
		t.Fatal(err)
	}
	if currentRateOID != originalRateOID {
		t.Fatal("upgrade rebuilt runtime_rate_limit on a pure-upstream database")
	}
	if got := originConstraintOID(t, ctx, pool); got == originOID {
		t.Fatal("743 left the weixin-only origin check in place")
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertRelation(t, ctx, pool, "doc")
	assertIndexValid(t, ctx, pool, "doc_folder_pkey")
	assertLedger(t, ctx, pool, "667_issue_origin_weixin_chat", true)
	assertLedger(t, ctx, pool, "685_docs", true)
	assertLedger(t, ctx, pool, "695_inbox_item_returned_at", true)
	assertLedger(t, ctx, pool, "696_inbox_item_active_shown_idx", true)
	assertLedger(t, ctx, pool, "756_issue_origin_sharecrm_chat", true)
	assertLedger(t, ctx, pool, "757_issue_origin_sharecrm_chat_validate", true)
	assertIndexValid(t, ctx, pool, "idx_inbox_active_shown")
	assertLedger(t, ctx, pool, "685_issue_origin_sharecrm_chat", false)
	assertLedger(t, ctx, pool, "686_issue_origin_sharecrm_chat_validate", false)
}

// TestShareCRM697698UpgradesThroughAutopilot is the previous fork tip.
// ShareCRM origin filenames 697/698 are already recorded and the origin
// check is validated. Upstream now uses 697/698 for autopilot_run's
// idempotency key and its unique index, 699-702 for the guest role and
// member_search_access, and the ShareCRM widen continues as 756/757.
// The new tail must not swap a check that already allows sharecrm_chat,
// and the retired ledger keys stay beside the new filenames.
func TestShareCRM697698UpgradesThroughAutopilot(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, true)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			));
		CREATE TABLE autopilot_run (
			id UUID PRIMARY KEY,
			autopilot_id UUID NOT NULL
		);
		CREATE TABLE member (
			id UUID PRIMARY KEY,
			role TEXT NOT NULL
		);
		ALTER TABLE member ADD CONSTRAINT member_role_check
			CHECK (role IN ('owner', 'admin', 'member'));
		CREATE TABLE workspace_invitation (
			id UUID PRIMARY KEY,
			role TEXT NOT NULL
		);
		ALTER TABLE workspace_invitation ADD CONSTRAINT workspace_invitation_role_check
			CHECK (role IN ('admin', 'member'));
		INSERT INTO member (id, role) VALUES ('00000000-0000-4000-8000-000000000091', 'member');
		INSERT INTO autopilot_run (id, autopilot_id) VALUES ('00000000-0000-4000-8000-000000000092', '00000000-0000-4000-8000-000000000093');
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, append(append([]string{}, historicalShareCRMOriginVersions...),
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
		"695_inbox_item_returned_at",
		"696_inbox_item_active_shown_idx",
	))
	before := snapshotOrigins(t, ctx, pool)
	originOID := originConstraintOID(t, ctx, pool)
	options := runOptions{
		Direction: "up",
		Files: realMigrationFiles(t, []string{
			"697_autopilot_run_idempotency_key",
			"698_autopilot_run_idempotency_key_idx",
			"699_member_guest_role",
			"700_member_search_access",
			"701_member_search_access_pkey_index",
			"702_member_search_access_pkey",
			"756_issue_origin_sharecrm_chat",
			"757_issue_origin_sharecrm_chat_validate",
		}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("second up: %v", err)
	}
	if got := originConstraintOID(t, ctx, pool); got != originOID {
		t.Fatalf("upgrade rebuilt an origin check that already allowed sharecrm_chat: oid %d -> %d", originOID, got)
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertRelation(t, ctx, pool, "member_search_access")
	assertIndexValid(t, ctx, pool, "uq_autopilot_run_idempotency_key")
	assertIndexValid(t, ctx, pool, "member_search_access_pkey")
	var idempotency bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'autopilot_run'
			  AND column_name = 'idempotency_key'
		)
	`).Scan(&idempotency); err != nil || !idempotency {
		t.Fatalf("autopilot_run.idempotency_key present = %t, error = %v", idempotency, err)
	}
	var guestAllowed bool
	if err := pool.QueryRow(ctx, `
		SELECT position('guest' IN pg_get_constraintdef(oid)) > 0
		FROM pg_constraint
		WHERE conrelid = 'member'::regclass AND conname = 'member_role_check'
	`).Scan(&guestAllowed); err != nil || !guestAllowed {
		t.Fatalf("member role allows guest = %t, error = %v", guestAllowed, err)
	}
	for _, version := range []string{
		"697_issue_origin_sharecrm_chat",
		"698_issue_origin_sharecrm_chat_validate",
		"697_autopilot_run_idempotency_key",
		"698_autopilot_run_idempotency_key_idx",
		"699_member_guest_role",
		"700_member_search_access",
		"701_member_search_access_pkey_index",
		"702_member_search_access_pkey",
		"703_issue_origin_sharecrm_chat",
		"704_issue_origin_sharecrm_chat_validate",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
	} {
		assertLedger(t, ctx, pool, version, true)
	}
}

// TestShareCRM703704UpgradesThroughDocComments is this morning's fork tip.
// ShareCRM origin filenames 703/704 are already recorded and the origin
// check is validated. Upstream now uses 703-707 for doc_comment, 708/709
// for the attachment workspace size index, and 710 for
// autopilot_quota_period.rejection_notified_limit. The ShareCRM widen
// continues as 756/757 and must not swap a check that already allows
// sharecrm_chat. The retired ledger keys stay beside the new filenames.
// An existing runtime_rate_limit keeps its OID and row.
func TestShareCRM703704UpgradesThroughDocComments(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, true)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			));
		CREATE TABLE runtime_rate_limit (
			runtime_id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			snapshot JSONB NOT NULL,
			observed_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		INSERT INTO runtime_rate_limit (runtime_id, workspace_id, snapshot, observed_at)
		VALUES ('00000000-0000-4000-8000-0000000000a1', '00000000-0000-4000-8000-0000000000a2', '{"kept":true}', now());
		CREATE TABLE attachment (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			size_bytes BIGINT NOT NULL
		);
		CREATE INDEX idx_attachment_workspace ON attachment (workspace_id);
		CREATE TABLE autopilot_quota_period (
			workspace_id UUID NOT NULL,
			period_start TIMESTAMPTZ NOT NULL,
			period_end TIMESTAMPTZ NOT NULL
		);
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, append(append([]string{}, historicalShareCRMOriginVersions...),
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
		"673_runtime_rate_limit",
		"697_autopilot_run_idempotency_key",
		"698_autopilot_run_idempotency_key_idx",
		"699_member_guest_role",
		"700_member_search_access",
		"701_member_search_access_pkey_index",
		"702_member_search_access_pkey",
	))
	var originalRateOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&originalRateOID); err != nil {
		t.Fatal(err)
	}
	before := snapshotOrigins(t, ctx, pool)
	originOID := originConstraintOID(t, ctx, pool)
	options := runOptions{
		Direction: "up",
		Files: realMigrationFiles(t, []string{
			"703_doc_comment",
			"704_doc_comment_pkey_index",
			"705_doc_comment_pkey",
			"706_doc_comment_doc_idx",
			"707_doc_comment_workspace_idx",
			"708_attachment_workspace_size_idx",
			"709_drop_attachment_workspace_index",
			"710_autopilot_quota_limit_notice",
			"756_issue_origin_sharecrm_chat",
			"757_issue_origin_sharecrm_chat_validate",
		}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("second up: %v", err)
	}
	var currentRateOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&currentRateOID); err != nil {
		t.Fatal(err)
	}
	if currentRateOID != originalRateOID {
		t.Fatal("upgrade rebuilt runtime_rate_limit on a database already at ShareCRM 703/704")
	}
	var snapshots int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_rate_limit WHERE snapshot->>'kept' = 'true'`).Scan(&snapshots); err != nil || snapshots != 1 {
		t.Fatalf("kept snapshots = %d, error = %v", snapshots, err)
	}
	if got := originConstraintOID(t, ctx, pool); got != originOID {
		t.Fatalf("upgrade rebuilt an origin check that already allowed sharecrm_chat: oid %d -> %d", originOID, got)
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertRelation(t, ctx, pool, "doc_comment")
	assertIndexValid(t, ctx, pool, "doc_comment_pkey")
	assertIndexValid(t, ctx, pool, "doc_comment_doc_idx")
	assertIndexValid(t, ctx, pool, "doc_comment_workspace_idx")
	assertIndexValid(t, ctx, pool, "idx_attachment_workspace_size")
	assertIndexAbsent(t, ctx, pool, "idx_attachment_workspace")
	var hasNotifiedLimit bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'autopilot_quota_period'
			  AND column_name = 'rejection_notified_limit'
		)
	`).Scan(&hasNotifiedLimit); err != nil || !hasNotifiedLimit {
		t.Fatalf("autopilot_quota_period.rejection_notified_limit present = %t, error = %v", hasNotifiedLimit, err)
	}
	for _, version := range []string{
		"703_issue_origin_sharecrm_chat",
		"704_issue_origin_sharecrm_chat_validate",
		"703_doc_comment",
		"704_doc_comment_pkey_index",
		"705_doc_comment_pkey",
		"706_doc_comment_doc_idx",
		"707_doc_comment_workspace_idx",
		"708_attachment_workspace_size_idx",
		"709_drop_attachment_workspace_index",
		"710_autopilot_quota_limit_notice",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
		"673_runtime_rate_limit",
	} {
		assertLedger(t, ctx, pool, version, true)
	}
}

// TestShareCRM711712UpgradesThroughDashboardRange is an older fork tip.
// ShareCRM origin filenames 711/712 are already recorded and the origin
// check is validated. Upstream now uses 711/712 for dashboard_range_view and
// uq_dashboard_range_view_scope. The ShareCRM widen continues as 756/757 and
// must not swap a check that already allows sharecrm_chat. The retired
// ledger keys stay beside the new filenames.
func TestShareCRM711712UpgradesThroughDashboardRange(t *testing.T) {
	ctx, pool, schema := newOriginSandbox(t)
	prepareShareCRMIssue(t, ctx, pool, true)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			));
	`); err != nil {
		t.Fatal(err)
	}
	insertLedger(t, ctx, pool, append([]string{}, historicalShareCRMOriginVersions...))
	before := snapshotOrigins(t, ctx, pool)
	originOID := originConstraintOID(t, ctx, pool)
	options := runOptions{
		Direction: "up",
		Files: realMigrationFiles(t, []string{
			"711_dashboard_range_view",
			"712_dashboard_range_view_scope_idx",
			"756_issue_origin_sharecrm_chat",
			"757_issue_origin_sharecrm_chat_validate",
		}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("second up: %v", err)
	}
	if got := originConstraintOID(t, ctx, pool); got != originOID {
		t.Fatalf("upgrade rebuilt an origin check that already allowed sharecrm_chat: oid %d -> %d", originOID, got)
	}
	assertOriginsKept(t, ctx, pool, before)
	assertNewChannelWrites(t, ctx, pool)
	assertRelation(t, ctx, pool, "dashboard_range_view")
	assertIndexValid(t, ctx, pool, "uq_dashboard_range_view_scope")
	for _, version := range []string{
		"711_issue_origin_sharecrm_chat",
		"712_issue_origin_sharecrm_chat_validate",
		"711_dashboard_range_view",
		"712_dashboard_range_view_scope_idx",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
	} {
		assertLedger(t, ctx, pool, version, true)
	}
}

// TestPreviousShareCRMTipUpgradesThroughCommentIdempotency is the post-#49
// working tip: every migration through 731, plus the retired ShareCRM ledger
// keys 732_issue_origin_sharecrm_chat and
// 733_issue_origin_sharecrm_chat_validate. Upstream reused 732-734 for
// comment idempotency receipts and 735-742 for task_usage_hourly squad
// attribution. Those files must run, and so must upstream 743-755. The new
// ShareCRM filenames 756/757 must record themselves without swapping the
// origin check, and a second upgrade must leave the ledger and the OID
// alone. The 735/736, 743/744, and 754/755 ShareCRM filenames are not on
// this database.
func TestPreviousShareCRMTipUpgradesThroughCommentIdempotency(t *testing.T) {
	upgradeFromRecordedShareCRMKeys(t, recordedShareCRMUpgrade{
		database: "origin_prev_732",
		skip: append([]string{
			"732_comment_idempotency_receipt",
			"733_comment_idempotency_receipt_key",
			"734_comment_idempotency_receipt_created",
			"735_task_usage_hourly_squad_col",
			"736_task_usage_hourly_squad_uq_idx",
			"737_task_usage_hourly_dirty_squad_uq_idx",
			"738_task_usage_hourly_workspace_squad_idx",
			"739_task_usage_hourly_squad_pipeline",
			"740_task_usage_hourly_drop_legacy_key",
			"741_task_usage_hourly_dirty_drop_legacy_key",
			"742_task_usage_hourly_squad_rollback_merge",
		}, migrationsAfter742...),
		recorded: []string{
			"732_issue_origin_sharecrm_chat",
			"733_issue_origin_sharecrm_chat_validate",
		},
		absentAfter: []string{
			"735_issue_origin_sharecrm_chat",
			"736_issue_origin_sharecrm_chat_validate",
			"743_issue_origin_sharecrm_chat",
			"744_issue_origin_sharecrm_chat_validate",
			"754_issue_origin_sharecrm_chat",
			"755_issue_origin_sharecrm_chat_validate",
		},
	})
}

// TestRecordedShareCRM735736UpgradesThroughSquadUsage is the unmerged PR #50
// tip: comment idempotency 732-734 is already applied, and the ShareCRM
// origin pair is recorded as 735_issue_origin_sharecrm_chat /
// 736_issue_origin_sharecrm_chat_validate. Upstream then reused 735-742 for
// squad usage. Those files must still run, and so must upstream 743-755.
// 756/757 must be a no-op for an origin check that already allows
// sharecrm_chat, and the retired 735/736 keys stay in the ledger. The
// 743/744 and 754/755 ShareCRM filenames are not on this database.
func TestRecordedShareCRM735736UpgradesThroughSquadUsage(t *testing.T) {
	upgradeFromRecordedShareCRMKeys(t, recordedShareCRMUpgrade{
		database: "origin_prev_735",
		skip: append([]string{
			"735_task_usage_hourly_squad_col",
			"736_task_usage_hourly_squad_uq_idx",
			"737_task_usage_hourly_dirty_squad_uq_idx",
			"738_task_usage_hourly_workspace_squad_idx",
			"739_task_usage_hourly_squad_pipeline",
			"740_task_usage_hourly_drop_legacy_key",
			"741_task_usage_hourly_dirty_drop_legacy_key",
			"742_task_usage_hourly_squad_rollback_merge",
		}, migrationsAfter742...),
		recorded: []string{
			"735_issue_origin_sharecrm_chat",
			"736_issue_origin_sharecrm_chat_validate",
		},
		absentAfter: []string{
			"732_issue_origin_sharecrm_chat",
			"733_issue_origin_sharecrm_chat_validate",
			"743_issue_origin_sharecrm_chat",
			"744_issue_origin_sharecrm_chat_validate",
			"754_issue_origin_sharecrm_chat",
			"755_issue_origin_sharecrm_chat_validate",
		},
	})
}

// TestRecordedShareCRM732And735UpgradesThroughSquadUsage is a database that
// recorded both retired ShareCRM pairs: 732/733 from the post-#49 tip and
// 735/736 from the unmerged PR #50 tip, including comment idempotency
// 732-734. Squad usage 735-742 and upstream 743-755 still apply under their
// own filenames, and 756/757 does not rebuild the origin check. The 743/744
// and 754/755 ShareCRM filenames are not on this database.
func TestRecordedShareCRM732And735UpgradesThroughSquadUsage(t *testing.T) {
	upgradeFromRecordedShareCRMKeys(t, recordedShareCRMUpgrade{
		database: "origin_prev_732_735",
		skip: append([]string{
			"735_task_usage_hourly_squad_col",
			"736_task_usage_hourly_squad_uq_idx",
			"737_task_usage_hourly_dirty_squad_uq_idx",
			"738_task_usage_hourly_workspace_squad_idx",
			"739_task_usage_hourly_squad_pipeline",
			"740_task_usage_hourly_drop_legacy_key",
			"741_task_usage_hourly_dirty_drop_legacy_key",
			"742_task_usage_hourly_squad_rollback_merge",
		}, migrationsAfter742...),
		recorded: []string{
			"732_issue_origin_sharecrm_chat",
			"733_issue_origin_sharecrm_chat_validate",
			"735_issue_origin_sharecrm_chat",
			"736_issue_origin_sharecrm_chat_validate",
		},
		absentAfter: []string{
			"743_issue_origin_sharecrm_chat",
			"744_issue_origin_sharecrm_chat_validate",
			"754_issue_origin_sharecrm_chat",
			"755_issue_origin_sharecrm_chat_validate",
		},
	})
}

// TestRecordedShareCRM743744UpgradesThroughBlockers is the previous PR #50
// tip: upstream through 742 is applied, and the ShareCRM origin pair is
// recorded as 743_issue_origin_sharecrm_chat /
// 744_issue_origin_sharecrm_chat_validate. Upstream then reused 743-755.
// Those files must still run. 756/757 must be a no-op for an origin check
// that already allows sharecrm_chat, and the retired 743/744 keys stay.
// The 754/755 ShareCRM filenames are not on this database.
func TestRecordedShareCRM743744UpgradesThroughBlockers(t *testing.T) {
	upgradeFromRecordedShareCRMKeys(t, recordedShareCRMUpgrade{
		database: "origin_prev_743",
		skip:     migrationsAfter742,
		recorded: []string{
			"743_issue_origin_sharecrm_chat",
			"744_issue_origin_sharecrm_chat_validate",
		},
		absentAfter: []string{
			"732_issue_origin_sharecrm_chat",
			"733_issue_origin_sharecrm_chat_validate",
			"735_issue_origin_sharecrm_chat",
			"736_issue_origin_sharecrm_chat_validate",
			"754_issue_origin_sharecrm_chat",
			"755_issue_origin_sharecrm_chat_validate",
		},
	})
}

// TestRecordedShareCRM732735743UpgradesThroughBlockers is a database that
// recorded retired ShareCRM pairs 732/733, 735/736, and 743/744. Comment
// idempotency, squad usage, and upstream 743-755 were not applied under
// their own filenames. Each of those files still runs, and 756/757 does not
// rebuild the origin check. The 754/755 ShareCRM filenames are not on this
// database.
func TestRecordedShareCRM732735743UpgradesThroughBlockers(t *testing.T) {
	upgradeFromRecordedShareCRMKeys(t, recordedShareCRMUpgrade{
		database: "origin_prev_732_735_743",
		skip: append([]string{
			"732_comment_idempotency_receipt",
			"733_comment_idempotency_receipt_key",
			"734_comment_idempotency_receipt_created",
			"735_task_usage_hourly_squad_col",
			"736_task_usage_hourly_squad_uq_idx",
			"737_task_usage_hourly_dirty_squad_uq_idx",
			"738_task_usage_hourly_workspace_squad_idx",
			"739_task_usage_hourly_squad_pipeline",
			"740_task_usage_hourly_drop_legacy_key",
			"741_task_usage_hourly_dirty_drop_legacy_key",
			"742_task_usage_hourly_squad_rollback_merge",
		}, migrationsAfter742...),
		recorded: []string{
			"732_issue_origin_sharecrm_chat",
			"733_issue_origin_sharecrm_chat_validate",
			"735_issue_origin_sharecrm_chat",
			"736_issue_origin_sharecrm_chat_validate",
			"743_issue_origin_sharecrm_chat",
			"744_issue_origin_sharecrm_chat_validate",
		},
		absentAfter: []string{
			"754_issue_origin_sharecrm_chat",
			"755_issue_origin_sharecrm_chat_validate",
		},
	})
}

// TestRecordedShareCRM754755UpgradesThroughStageBlockers is the previous
// merged tip (PR #50): upstream through 753 is applied, and the ShareCRM
// origin pair is recorded as 754_issue_origin_sharecrm_chat /
// 755_issue_origin_sharecrm_chat_validate. Upstream then reused 754/755 for
// the top-level blocker skip and the hourly usage rollup. Those files must
// still run. 756/757 must be a no-op for an origin check that already allows
// sharecrm_chat, and the retired 754/755 keys stay.
func TestRecordedShareCRM754755UpgradesThroughStageBlockers(t *testing.T) {
	upgradeFromRecordedShareCRMKeys(t, recordedShareCRMUpgrade{
		database: "origin_prev_754",
		skip:     migrationsAfter753,
		recorded: []string{
			"754_issue_origin_sharecrm_chat",
			"755_issue_origin_sharecrm_chat_validate",
		},
		absentAfter: []string{
			"732_issue_origin_sharecrm_chat",
			"733_issue_origin_sharecrm_chat_validate",
			"735_issue_origin_sharecrm_chat",
			"736_issue_origin_sharecrm_chat_validate",
			"743_issue_origin_sharecrm_chat",
			"744_issue_origin_sharecrm_chat_validate",
		},
	})
}

// TestRecordedShareCRM732735743754UpgradesThroughStageBlockers is a database
// that recorded every retired ShareCRM pair from this fork: 732/733, 735/736,
// 743/744, and 754/755. Comment idempotency, squad usage, upstream 743-755,
// and the new ShareCRM filenames 756/757 still run, and 756/757 does not
// rebuild the origin check.
func TestRecordedShareCRM732735743754UpgradesThroughStageBlockers(t *testing.T) {
	upgradeFromRecordedShareCRMKeys(t, recordedShareCRMUpgrade{
		database: "origin_prev_732_735_743_754",
		skip: append([]string{
			"732_comment_idempotency_receipt",
			"733_comment_idempotency_receipt_key",
			"734_comment_idempotency_receipt_created",
			"735_task_usage_hourly_squad_col",
			"736_task_usage_hourly_squad_uq_idx",
			"737_task_usage_hourly_dirty_squad_uq_idx",
			"738_task_usage_hourly_workspace_squad_idx",
			"739_task_usage_hourly_squad_pipeline",
			"740_task_usage_hourly_drop_legacy_key",
			"741_task_usage_hourly_dirty_drop_legacy_key",
			"742_task_usage_hourly_squad_rollback_merge",
		}, migrationsAfter742...),
		recorded: []string{
			"732_issue_origin_sharecrm_chat",
			"733_issue_origin_sharecrm_chat_validate",
			"735_issue_origin_sharecrm_chat",
			"736_issue_origin_sharecrm_chat_validate",
			"743_issue_origin_sharecrm_chat",
			"744_issue_origin_sharecrm_chat_validate",
			"754_issue_origin_sharecrm_chat",
			"755_issue_origin_sharecrm_chat_validate",
		},
	})
}

type recordedShareCRMUpgrade struct {
	database    string
	skip        []string
	recorded    []string
	absentAfter []string
}

func upgradeFromRecordedShareCRMKeys(t *testing.T, spec recordedShareCRMUpgrade) {
	t.Helper()
	if testing.Short() {
		t.Skip("postgres upgrade regression")
	}
	admin := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	name := fmt.Sprintf("%s_%d", spec.database, time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		_, _ = admin.Exec(dropCtx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = admin.Exec(dropCtx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize())
	})

	pool := openPoolURL(t, databaseURLFor(t, name))
	files, err := migrations.Files("up")
	if err != nil {
		t.Fatal(err)
	}
	skip := map[string]bool{}
	for _, version := range spec.skip {
		skip[version] = true
	}
	base := make([]string, 0, len(files))
	for _, file := range files {
		if skip[migrations.ExtractVersion(file)] {
			continue
		}
		base = append(base, file)
	}
	baseOptions := runOptions{
		Direction:             "up",
		Files:                 base,
		SchemaMigrationsTable: "schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
		Conditions:            conditionsForDirection("up"),
	}
	if err := runMigrations(ctx, pool, baseOptions); err != nil {
		t.Fatalf("previous tip schema: %v", err)
	}
	insertLedger(t, ctx, pool, spec.recorded)
	insertChannelIssue(t, ctx, pool, "sharecrm_chat")
	insertChannelIssue(t, ctx, pool, "weixin_chat")
	insertChannelIssue(t, ctx, pool, "slack_chat")
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config, installer_user_id
		) VALUES
			('00000000-0000-4000-8000-0000000000a1', '00000000-0000-4000-8000-0000000000a2', 'sharecrm', '{"app_id":"sharecrm-kept","app_secret_encrypted":"sealed"}', '00000000-0000-4000-8000-0000000000a3'),
			('00000000-0000-4000-8000-0000000000a1', '00000000-0000-4000-8000-0000000000a4', 'slack', '{"app_id":"slack-kept"}', '00000000-0000-4000-8000-0000000000a3'),
			('00000000-0000-4000-8000-0000000000a1', '00000000-0000-4000-8000-0000000000a5', 'weixin', '{"app_id":"weixin-kept"}', '00000000-0000-4000-8000-0000000000a3')
	`); err != nil {
		t.Fatalf("insert channel installations: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO runtime_rate_limit (runtime_id, workspace_id, snapshot, observed_at)
		VALUES ('00000000-0000-4000-8000-0000000000b1', '00000000-0000-4000-8000-0000000000a1', '{"kept":true}', now())
	`); err != nil {
		t.Fatalf("insert rate-limit snapshot: %v", err)
	}
	originOID := originConstraintOID(t, ctx, pool)
	var rateOID uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&rateOID); err != nil {
		t.Fatal(err)
	}
	var sharecrmBefore, installsBefore, snapshotsBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE origin_type = 'sharecrm_chat'`).Scan(&sharecrmBefore); err != nil || sharecrmBefore != 1 {
		t.Fatalf("sharecrm issues before upgrade = %d, error = %v", sharecrmBefore, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_installation WHERE config->>'app_id' IN ('sharecrm-kept', 'slack-kept', 'weixin-kept')`).Scan(&installsBefore); err != nil || installsBefore != 3 {
		t.Fatalf("installations before upgrade = %d, error = %v", installsBefore, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_rate_limit WHERE snapshot->>'kept' = 'true'`).Scan(&snapshotsBefore); err != nil || snapshotsBefore != 1 {
		t.Fatalf("kept rate-limit rows before upgrade = %d, error = %v", snapshotsBefore, err)
	}

	if err := runMigrations(ctx, pool, runOptions{
		Direction:             "up",
		Files:                 files,
		SchemaMigrationsTable: "schema_migrations",
		AdvisoryLockKey:       baseOptions.AdvisoryLockKey,
		Hooks:                 hooksForDirection("up"),
		Conditions:            conditionsForDirection("up"),
	}); err != nil {
		t.Fatalf("upgrade from previous tip: %v", err)
	}
	if got := originConstraintOID(t, ctx, pool); got != originOID {
		t.Fatalf("upgrade rebuilt an origin check that already allowed sharecrm_chat: oid %d -> %d", originOID, got)
	}
	var sharecrmAfter, installsAfter, snapshotsAfter int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE origin_type = 'sharecrm_chat'`).Scan(&sharecrmAfter); err != nil || sharecrmAfter != 1 {
		t.Fatalf("sharecrm issues after upgrade = %d, error = %v", sharecrmAfter, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_installation WHERE config->>'app_id' IN ('sharecrm-kept', 'slack-kept', 'weixin-kept')`).Scan(&installsAfter); err != nil || installsAfter != 3 {
		t.Fatalf("kept installations after upgrade = %d, error = %v", installsAfter, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_rate_limit WHERE snapshot->>'kept' = 'true'`).Scan(&snapshotsAfter); err != nil || snapshotsAfter != 1 {
		t.Fatalf("kept rate-limit rows after upgrade = %d, error = %v", snapshotsAfter, err)
	}
	assertRelation(t, ctx, pool, "comment_idempotency_receipt")
	assertIndexValid(t, ctx, pool, "uq_comment_idempotency_receipt_key")
	assertIndexValid(t, ctx, pool, "idx_comment_idempotency_receipt_created")
	assertSquadUsageShape(t, ctx, pool)
	assertBlockerAndQuotaShape(t, ctx, pool)
	assertStageBlockerSkipAndUsageRollup(t, ctx, pool)
	assertValidatedChannelOrigins(t, ctx, pool)
	present := append([]string{}, spec.recorded...)
	present = append(present,
		"732_comment_idempotency_receipt",
		"733_comment_idempotency_receipt_key",
		"734_comment_idempotency_receipt_created",
		"735_task_usage_hourly_squad_col",
		"736_task_usage_hourly_squad_uq_idx",
		"737_task_usage_hourly_dirty_squad_uq_idx",
		"738_task_usage_hourly_workspace_squad_idx",
		"739_task_usage_hourly_squad_pipeline",
		"740_task_usage_hourly_drop_legacy_key",
		"741_task_usage_hourly_dirty_drop_legacy_key",
		"742_task_usage_hourly_squad_rollback_merge",
		"673_runtime_rate_limit",
	)
	present = append(present, migrationsAfter742...)
	for _, version := range present {
		assertLedger(t, ctx, pool, version, true)
	}
	for _, version := range spec.absentAfter {
		assertLedger(t, ctx, pool, version, false)
	}
	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, runOptions{
		Direction:             "up",
		Files:                 files,
		SchemaMigrationsTable: "schema_migrations",
		AdvisoryLockKey:       baseOptions.AdvisoryLockKey,
		Hooks:                 hooksForDirection("up"),
		Conditions:            conditionsForDirection("up"),
	}); err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("second upgrade recorded %d versions, had %d", after, before)
	}
	if got := originConstraintOID(t, ctx, pool); got != originOID {
		t.Fatalf("second upgrade rebuilt the origin check: oid %d -> %d", originOID, got)
	}
	var rateAfter uint32
	if err := pool.QueryRow(ctx, `SELECT 'runtime_rate_limit'::regclass::oid`).Scan(&rateAfter); err != nil {
		t.Fatal(err)
	}
	if rateAfter != rateOID {
		t.Fatalf("upgrade rebuilt runtime_rate_limit: oid %d -> %d", rateOID, rateAfter)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_rate_limit WHERE snapshot->>'kept' = 'true'`).Scan(&snapshotsAfter); err != nil || snapshotsAfter != 1 {
		t.Fatalf("kept rate-limit rows after second upgrade = %d, error = %v", snapshotsAfter, err)
	}
}
func prepareShareCRMIssue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, validated bool) {
	t.Helper()
	validate := ""
	if validated {
		validate = "ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;"
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE issue (id UUID PRIMARY KEY, origin_type TEXT NULL);
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'sharecrm_chat',
				'custom_origin', 'weird''origin'
			)) NOT VALID;
		`+validate+`
		INSERT INTO issue (id, origin_type) VALUES
			('00000000-0000-4000-8000-000000000011', 'sharecrm_chat'),
			('00000000-0000-4000-8000-000000000012', 'custom_origin'),
			('00000000-0000-4000-8000-000000000013', 'weird''origin')
	`); err != nil {
		t.Fatal(err)
	}
}

func prepareWeixinOnlyIssue(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		CREATE TABLE issue (id UUID PRIMARY KEY, origin_type TEXT NULL);
		ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
			CHECK (origin_type IN (
				'autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create',
				'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat'
			));
		INSERT INTO issue (id, origin_type) VALUES
			('00000000-0000-4000-8000-000000000014', 'weixin_chat')
	`); err != nil {
		t.Fatal(err)
	}
}

func runOriginMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) {
	t.Helper()
	versions := []string{
		"667_issue_origin_weixin_chat",
		"668_issue_origin_weixin_chat_validate",
		"756_issue_origin_sharecrm_chat",
		"757_issue_origin_sharecrm_chat_validate",
	}
	if err := runMigrations(ctx, pool, runOptions{
		Direction:             "up",
		Files:                 realMigrationFiles(t, versions, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       testLockKey(),
	}); err != nil {
		t.Fatal(err)
	}
}

func snapshotOrigins(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT id::text, origin_type FROM issue`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, origin string
		if err := rows.Scan(&id, &origin); err != nil {
			t.Fatal(err)
		}
		out[id] = origin
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("fixture inserted no issue rows")
	}
	return out
}

func assertOriginsKept(t *testing.T, ctx context.Context, pool *pgxpool.Pool, before map[string]string) {
	t.Helper()
	assertValidatedChannelOrigins(t, ctx, pool)
	for id, origin := range before {
		var got string
		if err := pool.QueryRow(ctx, `SELECT origin_type FROM issue WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("read kept issue %s: %v", id, err)
		}
		if got != origin {
			t.Fatalf("issue %s origin = %q, want %q", id, got, origin)
		}
	}
}

func assertNewChannelWrites(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, origin := range []string{"sharecrm_chat", "weixin_chat", "custom_origin"} {
		if origin == "custom_origin" && !originListContains(t, ctx, pool, origin) {
			continue
		}
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", rand.Uint32())
		if _, err := pool.Exec(ctx, `INSERT INTO issue (id, origin_type) VALUES ($1, $2)`, id, origin); err != nil {
			t.Fatalf("insert %s: %v", origin, err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
}

func assertValidatedChannelOrigins(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var validated bool
	var definition string
	if err := pool.QueryRow(ctx, `
		SELECT convalidated, pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = to_regclass('issue') AND conname = 'issue_origin_type_check'
	`).Scan(&validated, &definition); err != nil {
		t.Fatal(err)
	}
	if !validated {
		t.Fatalf("origin check not validated: %s", definition)
	}
	for _, origin := range []string{"weixin_chat", "sharecrm_chat"} {
		if !strings.Contains(definition, "'"+origin+"'") {
			t.Fatalf("origin check missing %s: %s", origin, definition)
		}
	}
}

func originListContains(t *testing.T, ctx context.Context, pool *pgxpool.Pool, origin string) bool {
	t.Helper()
	var definition string
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = to_regclass('issue') AND conname = 'issue_origin_type_check'
	`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	return strings.Contains(definition, "'"+strings.ReplaceAll(origin, "'", "''")+"'")
}

func originConstraintOID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uint32 {
	t.Helper()
	var oid uint32
	if err := pool.QueryRow(ctx, `
		SELECT oid FROM pg_constraint
		WHERE conrelid = to_regclass('issue') AND conname = 'issue_origin_type_check'
	`).Scan(&oid); err != nil {
		t.Fatal(err)
	}
	return oid
}

func insertLedger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versions []string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		t.Fatal(err)
	}
	for _, version := range versions {
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, version); err != nil {
			t.Fatal(err)
		}
	}
}

func assertLedger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, version string, want bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ledger %s recorded = %t, want %t", version, got, want)
	}
}

func assertStageBlockerSkipAndUsageRollup(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var blocker, rollup string
	if err := pool.QueryRow(ctx, `SELECT pg_get_functiondef('record_issue_stage_blockers()'::regprocedure)`).Scan(&blocker); err != nil || !strings.Contains(blocker, "NEW.parent_issue_id IS NULL") {
		t.Fatalf("record_issue_stage_blockers missing top-level skip, error = %v, def = %s", err, blocker)
	}
	if err := pool.QueryRow(ctx, `SELECT pg_get_functiondef('rollup_task_usage_hourly_window(timestamptz,timestamptz)'::regprocedure)`).Scan(&rollup); err != nil || !strings.Contains(rollup, "dirty_hour_keys") {
		t.Fatalf("rollup_task_usage_hourly_window missing hour-key recompute, error = %v, def = %s", err, rollup)
	}
}

func assertBlockerAndQuotaShape(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	assertRelation(t, ctx, pool, "issue_blocker")
	assertIndexValid(t, ctx, pool, "issue_blocker_unique")
	assertIndexValid(t, ctx, pool, "issue_blocker_reverse")
	assertRelation(t, ctx, pool, "app_tool_call_quota_period")
	assertIndexValid(t, ctx, pool, "uq_app_tool_call_quota_period_scope")
	assertIndexValid(t, ctx, pool, "idx_app_tool_call_workspace_metered")
	assertIndexValid(t, ctx, pool, "idx_agent_task_queue_doc")
	var handoff string
	if err := pool.QueryRow(ctx, `
		SELECT column_default
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'squad'
		  AND column_name = 'handoff_mode'
	`).Scan(&handoff); err != nil || !strings.Contains(handoff, "leader") {
		t.Fatalf("squad.handoff_mode default = %q, error = %v", handoff, err)
	}
	var docID bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'agent_task_queue'
			  AND column_name = 'doc_id'
		)
	`).Scan(&docID); err != nil || !docID {
		t.Fatalf("agent_task_queue.doc_id present = %t, error = %v", docID, err)
	}
}

func assertSquadUsageShape(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, column := range []struct{ table, name string }{
		{"task_usage_hourly", "squad_id"},
		{"task_usage_hourly_dirty", "squad_id"},
		{"agent_task_queue", "usage_squad_id"},
	} {
		var present bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = current_schema()
				  AND table_name = $1
				  AND column_name = $2
			)
		`, column.table, column.name).Scan(&present); err != nil || !present {
			t.Fatalf("%s.%s present = %t, error = %v", column.table, column.name, present, err)
		}
	}
	assertIndexValid(t, ctx, pool, "uq_task_usage_hourly_squad_key_idx")
	assertIndexValid(t, ctx, pool, "uq_task_usage_hourly_dirty_squad_key_idx")
	assertIndexValid(t, ctx, pool, "idx_task_usage_hourly_workspace_squad_time")
}

func assertRelation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil || !exists {
		t.Fatalf("relation %s exists = %t, error = %v", name, exists, err)
	}
}

func assertIndexAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema()
			  AND c.relname = $1
		)
	`, name).Scan(&exists); err != nil || exists {
		t.Fatalf("index %s still exists = %t, error = %v", name, exists, err)
	}
}

func assertIndexValid(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `
		SELECT i.indisvalid
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relname = $1
	`, name).Scan(&valid); err != nil || !valid {
		t.Fatalf("index %s valid = %t, error = %v", name, valid, err)
	}
}

func assertUniqueLedgerPrefixes(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		prefix, _, ok := strings.Cut(version, "_")
		if !ok {
			continue
		}
		number, err := strconv.Atoi(prefix)
		if err != nil || number < 129 {
			continue
		}
		if previous, exists := seen[prefix]; exists {
			t.Errorf("schema_migrations versions %s and %s share numeric prefix %s", previous, version, prefix)
		}
		seen[prefix] = version
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func insertChannelIssue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, origin string) {
	t.Helper()
	userID := insertRequiredRow(t, ctx, pool, "user", map[string]string{
		"name":  "'Origin Upgrade'",
		"email": "'" + origin + "-" + fmt.Sprint(time.Now().UnixNano()) + "@example.com'",
	})
	workspaceID := insertRequiredRow(t, ctx, pool, "workspace", map[string]string{
		"name": "'Origin Upgrade'",
		"slug": "'" + origin + "-" + fmt.Sprint(time.Now().UnixNano()) + "'",
	})
	insertRequiredRow(t, ctx, pool, "issue", map[string]string{
		"workspace_id": "'" + workspaceID + "'",
		"title":        "'channel origin'",
		"creator_type": "'member'",
		"creator_id":   "'" + userID + "'",
		"origin_type":  "'" + origin + "'",
	})
}

func insertRequiredRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, known map[string]string) string {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT column_name, udt_name, is_nullable, column_default IS NOT NULL
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1
		ORDER BY ordinal_position
	`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols, vals []string
	for rows.Next() {
		var name, udt, nullable string
		var hasDefault bool
		if err := rows.Scan(&name, &udt, &nullable, &hasDefault); err != nil {
			t.Fatal(err)
		}
		if expr, ok := known[name]; ok {
			cols = append(cols, pgx.Identifier{name}.Sanitize())
			vals = append(vals, expr)
			continue
		}
		if nullable == "YES" || hasDefault {
			continue
		}
		cols = append(cols, pgx.Identifier{name}.Sanitize())
		vals = append(vals, literalForRequiredColumn(t, name, udt))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var id string
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING id",
		pgx.Identifier{table}.Sanitize(), strings.Join(cols, ", "), strings.Join(vals, ", "))
	if err := pool.QueryRow(ctx, query).Scan(&id); err != nil {
		t.Fatalf("insert %s: %v\n%s", table, err, query)
	}
	return id
}

func literalForRequiredColumn(t *testing.T, name, udt string) string {
	t.Helper()
	switch udt {
	case "uuid":
		return "'00000000-0000-4000-8000-0000000000ab'"
	case "text", "varchar":
		return "'x'"
	case "jsonb":
		return "'{}'"
	case "bool":
		return "false"
	case "int2", "int4", "int8":
		return "0"
	case "float4", "float8":
		return "0"
	case "timestamptz", "timestamp":
		return "now()"
	default:
		t.Fatalf("required column %s has unsupported type %s", name, udt)
		return ""
	}
}

func newOriginSandbox(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
	admin := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	schema := fmt.Sprintf("origin_up_%d_%d", time.Now().UnixNano(), rand.Uint32())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		_, _ = admin.Exec(dropCtx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	})
	return ctx, openTestPoolWithSearchPath(t, schema), schema
}

func openPoolURL(t *testing.T, dbURL string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func databaseURLFor(t *testing.T, name string) string {
	t.Helper()
	parsed, err := url.Parse(testDatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}

func testLockKey() int64 {
	return int64(rand.Uint64()&0x7fffffffffffffff) | 1
}
