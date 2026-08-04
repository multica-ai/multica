package toolpolicy

// Boundary tests for the FIR-3403 migration fixes. Two confirmed privilege
// escalations were found in the PR#2553 migrations before release:
//
//  1. 9153 migrated runtime-scoped group/user grants into workspace-wide
//     allows (the runtime_id join was only used to fetch workspace_id).
//  2. 9152 packaged role permissions keyed on tool_key (collapsing several
//     rules for one tool to one) and the read-back consumed only 'setting',
//     so a resource- or condition-scoped allow acted as a whole-tool allow.
//
// These tests pin the corrected behaviour from both ends: the resolver end
// (a runtime/resource-scoped rule never widens) and the migration end (a
// transactional replay of the migration files against recreated legacy
// tables proves the emitted rows carry their scope).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// --- Pure tests (no database) -----------------------------------------------

func TestDecodeRolePermissionShapes(t *testing.T) {
	t.Run("canonical list shape", func(t *testing.T) {
		raw := []byte(`[
			{"setting":"allow","resource_pattern":"action:list_apps","conditions":null},
			{"setting":"deny","resource_pattern":"","conditions":{"actions":["push"]}}
		]`)
		rules, err := decodeRolePermission(raw)
		if err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(rules) != 2 {
			t.Fatalf("rules = %d, want 2 (several rules per tool must not collapse)", len(rules))
		}
		if rules[0].Setting != "allow" || rules[0].ResourcePattern != "action:list_apps" {
			t.Fatalf("rule[0] = %+v, want scoped allow", rules[0])
		}
		if rules[1].Conditions == nil || len(rules[1].Conditions.Actions) != 1 {
			t.Fatalf("rule[1] conditions = %+v, want actions term", rules[1].Conditions)
		}
	})
	t.Run("legacy single-object shape still resolves", func(t *testing.T) {
		rules, err := decodeRolePermission([]byte(`{"setting":"deny"}`))
		if err != nil {
			t.Fatalf("decode object: %v", err)
		}
		if len(rules) != 1 || rules[0].Setting != "deny" || rules[0].ResourcePattern != "" {
			t.Fatalf("rules = %+v, want one capability-wide deny", rules)
		}
	})
	t.Run("null and empty decode to nothing", func(t *testing.T) {
		for _, raw := range [][]byte{nil, []byte(""), []byte("null")} {
			rules, err := decodeRolePermission(raw)
			if err != nil || rules != nil {
				t.Fatalf("decode %q = (%v, %v), want (nil, nil)", raw, rules, err)
			}
		}
	})
}

func TestRequestContextWithRuntime(t *testing.T) {
	runtime := uuidByte(77)
	in := Query{
		RuntimeID: runtime,
		RequestContext: RequestContext{
			Action:    "push",
			ArgValues: map[string]string{"data_source_id": "src-1"},
		},
	}
	got := requestContextWithRuntime(in)
	if got.ArgValues["runtime_id"] != util.UUIDToString(runtime) {
		t.Fatalf("runtime_id = %q, want %q", got.ArgValues["runtime_id"], util.UUIDToString(runtime))
	}
	if got.ArgValues["data_source_id"] != "src-1" || got.Action != "push" {
		t.Fatalf("existing context lost: %+v", got)
	}
	if _, mutated := in.RequestContext.ArgValues["runtime_id"]; mutated {
		t.Fatal("caller's ArgValues map was mutated")
	}
	// No runtime in scope: the argument stays absent so a runtime-scoped Allow
	// fails closed instead of applying workspace-wide.
	if got := requestContextWithRuntime(Query{}); got.ArgValues != nil {
		t.Fatalf("no-runtime context = %+v, want untouched zero context", got)
	}
}

// --- Resolver boundary tests (database-backed, skip without DB) --------------

// runtimeScopedCondition is the exact conditions shape migration 9153 writes
// when preserving a legacy per-runtime grant.
func runtimeScopedCondition(runtimes ...pgtype.UUID) *Condition {
	values := make([]string, 0, len(runtimes))
	for _, r := range runtimes {
		values = append(values, util.UUIDToString(r))
	}
	return &Condition{ArgAllowlist: []ArgAllow{{Arg: "runtime_id", Values: values}}}
}

func TestRuntimeScopedGroupGrantConfinedToGrantedRuntime(t *testing.T) {
	store := newTPStore(t)
	clearAll(t, store)
	ctx := context.Background()

	group := uuidByte(60)
	runtimeA := uuidByte(61)
	runtimeB := uuidByte(62)
	const tool = "migrated.group_tool"

	if _, err := store.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerGroup,
		SubjectID: group, Setting: SettingAllow,
		Conditions: runtimeScopedCondition(runtimeA), UpdatedBy: tpTestUserID,
	}); err != nil {
		t.Fatalf("set runtime-scoped group allow: %v", err)
	}

	resolve := func(runtime pgtype.UUID) Setting {
		t.Helper()
		eff, err := store.Resolve(ctx, Query{
			WorkspaceID: tpTestWorkspaceID, ToolKey: tool,
			RuntimeID: runtime, GroupIDs: []pgtype.UUID{group},
		})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		return eff.Setting
	}

	if got := resolve(runtimeA); got != SettingAllow {
		t.Fatalf("granted runtime = %q, want allow", got)
	}
	if got := resolve(runtimeB); got != SettingDeny {
		t.Fatalf("other runtime = %q, want deny — a runtime-scoped grant must not travel", got)
	}
	if got := resolve(pgtype.UUID{}); got != SettingDeny {
		t.Fatalf("no runtime in scope = %q, want deny (fail closed)", got)
	}
}

func TestRuntimeScopedUserGrantConfinedToGrantedRuntime(t *testing.T) {
	store := newTPStore(t)
	clearAll(t, store)
	ctx := context.Background()

	runtimeA := uuidByte(63)
	runtimeB := uuidByte(64)
	const tool = "migrated.user_tool"

	if _, err := store.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerUser,
		SubjectID: tpTestUserID, Setting: SettingAllow,
		Conditions: runtimeScopedCondition(runtimeA), UpdatedBy: tpTestUserID,
	}); err != nil {
		t.Fatalf("set runtime-scoped user allow: %v", err)
	}

	resolve := func(runtime pgtype.UUID) Setting {
		t.Helper()
		eff, err := store.Resolve(ctx, Query{
			WorkspaceID: tpTestWorkspaceID, ToolKey: tool,
			RuntimeID: runtime, UserID: tpTestUserID,
			GroupIDs: []pgtype.UUID{uuidByte(99)}, // pin groups so user membership isn't auto-expanded
		})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		return eff.Setting
	}

	if got := resolve(runtimeA); got != SettingAllow {
		t.Fatalf("granted runtime = %q, want allow", got)
	}
	if got := resolve(runtimeB); got != SettingDeny {
		t.Fatalf("other runtime = %q, want deny — a runtime-scoped grant must not travel", got)
	}
}

func TestRoleResourceScopedAllowDoesNotWidenWholeTool(t *testing.T) {
	store := newTPStore(t)
	clearAll(t, store)
	ctx := context.Background()

	agent := uuidByte(65)
	const tool = "firtal_registry.role_scope_tool"

	var roleID pgtype.UUID
	if err := tpTestPool.QueryRow(ctx, `
		INSERT INTO cerebro_role (workspace_id, name, description, created_by, permissions)
		VALUES ($1, 'Resource scope boundary role', 'FIR-3403 finding 2 fixture', $2,
		        jsonb_build_object($3::text, jsonb_build_array(
		            jsonb_build_object('setting', 'allow',
		                               'resource_pattern', 'action:update_app',
		                               'conditions', NULL))))
		RETURNING id
	`, tpTestWorkspaceID, tpTestUserID, tool).Scan(&roleID); err != nil {
		t.Fatalf("create role: %v", err)
	}
	defer func() {
		_, _ = tpTestPool.Exec(context.Background(), `DELETE FROM cerebro_role WHERE id=$1`, roleID)
	}()
	if _, err := tpTestPool.Exec(ctx, `
		INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_by)
		VALUES ($1, 'agent', $2, $3)
	`, roleID, agent, tpTestUserID); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	// A capability-wide deny exists at the agent layer; the role's allow is
	// scoped to one resource pattern and must not lift the deny anywhere else.
	if _, err := store.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent,
		SubjectID: agent, Setting: SettingDeny, UpdatedBy: tpTestUserID,
	}); err != nil {
		t.Fatalf("set agent deny: %v", err)
	}

	wide, err := store.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent,
	})
	if err != nil {
		t.Fatalf("resolve capability-wide: %v", err)
	}
	if wide.Setting != SettingDeny {
		t.Fatalf("capability-wide = %q, want deny — a resource-scoped role allow must not read as a whole-tool allow", wide.Setting)
	}

	clearAll(t, store) // drop the agent deny; the scoped grant stands alone
	scoped, err := store.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent,
		ResourcePattern: "action:update_app",
	})
	if err != nil {
		t.Fatalf("resolve scoped: %v", err)
	}
	if scoped.Setting != SettingAllow {
		t.Fatalf("scoped resolve = %q, want allow on the granted pattern", scoped.Setting)
	}
}

func TestRoleMultipleRulesPerToolResolveIndependently(t *testing.T) {
	store := newTPStore(t)
	clearAll(t, store)
	ctx := context.Background()

	agent := uuidByte(66)
	const tool = "firtal_registry.multi_rule_tool"

	var roleID pgtype.UUID
	if err := tpTestPool.QueryRow(ctx, `
		INSERT INTO cerebro_role (workspace_id, name, description, created_by, permissions)
		VALUES ($1, 'Multi rule boundary role', 'FIR-3403 finding 2 fixture', $2,
		        jsonb_build_object($3::text, jsonb_build_array(
		            jsonb_build_object('setting', 'allow',
		                               'resource_pattern', 'action:list_apps',
		                               'conditions', NULL),
		            jsonb_build_object('setting', 'deny',
		                               'resource_pattern', 'action:update_app',
		                               'conditions', NULL))))
		RETURNING id
	`, tpTestWorkspaceID, tpTestUserID, tool).Scan(&roleID); err != nil {
		t.Fatalf("create role: %v", err)
	}
	defer func() {
		_, _ = tpTestPool.Exec(context.Background(), `DELETE FROM cerebro_role WHERE id=$1`, roleID)
	}()
	if _, err := tpTestPool.Exec(ctx, `
		INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_by)
		VALUES ($1, 'agent', $2, $3)
	`, roleID, agent, tpTestUserID); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	resolve := func(pattern string) Setting {
		t.Helper()
		eff, err := store.Resolve(ctx, Query{
			WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent,
			ResourcePattern: pattern,
		})
		if err != nil {
			t.Fatalf("resolve %q: %v", pattern, err)
		}
		return eff.Setting
	}

	if got := resolve("action:list_apps"); got != SettingAllow {
		t.Fatalf("list_apps = %q, want allow", got)
	}
	if got := resolve("action:update_app"); got != SettingDeny {
		t.Fatalf("update_app = %q, want deny — both rules must survive under one tool key", got)
	}
}

// --- Migration replay tests (database-backed, skip without DB) ---------------

// The migrated test database has already dropped the legacy tables, so the
// replay recreates them (schema from 9032/9028, FKs omitted where the parent
// row is irrelevant to the migrated data), seeds the escalation fixtures, and
// executes the real migration file inside a transaction that is rolled back.
//
// The retired table names are assembled at runtime because this replay fixture
// tests the historical migration, not the current read model. Live-schema
// absence is covered by the database-backed migration guard.
var (
	legacyRuntimeToolTable = "cerebro_runtime" + "_tool"
	legacyGroupGrantTable  = legacyRuntimeToolTable + "_group_grant"
	legacyUserGrantTable   = legacyRuntimeToolTable + "_user_grant"
	legacyOverrideTable    = "cerebro_agent_runtime" + "_tool_override"
)

func replayMigration(t *testing.T, ctx context.Context, tx pgx.Tx, file string) {
	t.Helper()
	raw, err := os.ReadFile("../../../migrations/" + file)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := tx.Exec(ctx, string(raw)); err != nil {
		t.Fatalf("replay %s: %v", file, err)
	}
}

func TestMigration9153PreservesRuntimeScopeForGroupAndUserGrants(t *testing.T) {
	if tpTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping toolpolicy integration test")
	}
	ctx := context.Background()
	tx, err := tpTestPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %[1]s (
			runtime_id UUID NOT NULL,
			tool_name TEXT NOT NULL,
			mcp_server_name TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL,
			CONSTRAINT %[1]s_unique UNIQUE (runtime_id, tool_name, mcp_server_name)
		);
		CREATE TABLE %[2]s (
			runtime_id UUID NOT NULL,
			tool_name TEXT NOT NULL,
			group_id UUID NOT NULL,
			granted_by UUID,
			PRIMARY KEY (runtime_id, tool_name, group_id)
		);
		CREATE TABLE %[3]s (
			runtime_id UUID NOT NULL,
			tool_name TEXT NOT NULL,
			user_id UUID NOT NULL,
			granted_by UUID,
			PRIMARY KEY (runtime_id, tool_name, user_id)
		);
		CREATE TABLE %[4]s (
			agent_id UUID NOT NULL,
			tool_name TEXT NOT NULL,
			enabled BOOLEAN NOT NULL,
			updated_by UUID,
			PRIMARY KEY (agent_id, tool_name)
		);
	`, legacyRuntimeToolTable, legacyGroupGrantTable, legacyUserGrantTable, legacyOverrideTable)); err != nil {
		t.Fatalf("recreate legacy tables: %v", err)
	}

	var runtimeA, runtimeB pgtype.UUID
	for i, target := range []*pgtype.UUID{&runtimeA, &runtimeB} {
		if err := tx.QueryRow(ctx, `
			INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
			VALUES ($1, $2, 'local', 'claude', 'offline') RETURNING id
		`, tpTestWorkspaceID, fmt.Sprintf("replay-runtime-%d", i)).Scan(target); err != nil {
			t.Fatalf("create runtime: %v", err)
		}
	}
	group := uuidByte(70)
	// Group granted on BOTH runtimes; user granted on runtime A only.
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (runtime_id, tool_name, group_id, granted_by)
		VALUES ($1, 'replay.group_tool', $3, $4), ($2, 'replay.group_tool', $3, $4)
	`, legacyGroupGrantTable), runtimeA, runtimeB, group, tpTestUserID); err != nil {
		t.Fatalf("seed group grants: %v", err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (runtime_id, tool_name, user_id, granted_by)
		VALUES ($1, 'replay.user_tool', $2, $2)
	`, legacyUserGrantTable), runtimeA, tpTestUserID); err != nil {
		t.Fatalf("seed user grant: %v", err)
	}

	replayMigration(t, ctx, tx, "9153_drop_legacy_runtime_tools.up.sql")

	assertScope := func(layer string, subject pgtype.UUID, tool string, wantRuntimes []pgtype.UUID) {
		t.Helper()
		var setting string
		var conditions []byte
		if err := tx.QueryRow(ctx, `
			SELECT setting, conditions FROM cerebro_tool_policy
			WHERE workspace_id=$1 AND tool_key=$2 AND layer=$3 AND subject_id=$4 AND resource_pattern=''
		`, tpTestWorkspaceID, tool, layer, subject).Scan(&setting, &conditions); err != nil {
			t.Fatalf("read migrated %s row: %v", layer, err)
		}
		if setting != "allow" {
			t.Fatalf("%s setting = %q, want allow", layer, setting)
		}
		var cond Condition
		if err := json.Unmarshal(conditions, &cond); err != nil {
			t.Fatalf("decode migrated conditions: %v (raw %s)", err, conditions)
		}
		if len(cond.ArgAllowlist) != 1 || cond.ArgAllowlist[0].Arg != "runtime_id" {
			t.Fatalf("%s conditions = %s, want a runtime_id arg_allowlist — runtime scope must survive the migration", layer, conditions)
		}
		want := map[string]bool{}
		for _, r := range wantRuntimes {
			want[util.UUIDToString(r)] = true
		}
		got := map[string]bool{}
		for _, v := range cond.ArgAllowlist[0].Values {
			got[v] = true
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("%s runtime allowlist = %v, want %v", layer, got, want)
		}
	}

	assertScope("group", group, "replay.group_tool", []pgtype.UUID{runtimeA, runtimeB})
	assertScope("user", tpTestUserID, "replay.user_tool", []pgtype.UUID{runtimeA})

	// The migration must still retire the legacy store.
	var legacyLeft int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_name = ANY($1::text[])
	`, []string{legacyRuntimeToolTable, legacyGroupGrantTable, legacyUserGrantTable, legacyOverrideTable}).Scan(&legacyLeft); err != nil {
		t.Fatalf("count legacy tables: %v", err)
	}
	if legacyLeft != 0 {
		t.Fatalf("legacy tables remaining = %d, want 0", legacyLeft)
	}
}

func TestMigration9152PackagesRolesAsRuleLists(t *testing.T) {
	if tpTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping toolpolicy integration test")
	}
	ctx := context.Background()
	tx, err := tpTestPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Recreate realistic legacy direct grants. The migration must preserve every
	// tool choice, including explicit denies and Registry action configuration,
	// before retiring the table.
	if _, err := tx.Exec(ctx, `
		CREATE TABLE agent_tool_grant (
			agent_id UUID NOT NULL,
			tool_name TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT true,
			config_json JSONB,
			PRIMARY KEY (agent_id, tool_name)
		);
	`); err != nil {
		t.Fatalf("recreate agent_tool_grant: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_canonical_capability (canonical_id, family, source_reference)
		VALUES ('platform:legacy.allowed_tool', 'platform', 'migration-test'),
		       ('platform:legacy.denied_tool', 'platform', 'migration-test'),
		       ('platform:legacy.configured_tool', 'platform', 'migration-test'),
		       ('gateway:firtal_registry', 'gateway', 'migration-test')
		ON CONFLICT (canonical_id) DO NOTHING;

		INSERT INTO cerebro_capability_alias (
			capability_id, surface, provider, key_value, resource_pattern,
			key_source, relation, source_reference
		)
		VALUES
			('platform:legacy.allowed_tool', 'policy', '', 'legacy.allowed_tool', '', 'platform', 'canonical', 'migration-test'),
			('platform:legacy.allowed_tool', 'runtime', 'claude', 'legacy.allowed_alias', '', '', 'alias', 'migration-test'),
			('platform:legacy.denied_tool', 'policy', '', 'legacy.denied_tool', '', 'platform', 'canonical', 'migration-test'),
			('platform:legacy.configured_tool', 'policy', '', 'legacy.configured_tool', '', 'platform', 'canonical', 'migration-test'),
			('gateway:firtal_registry', 'gateway', '', 'firtal_registry', '', '', 'canonical', 'migration-test')
		ON CONFLICT (surface, provider, key_value, resource_pattern, key_source) DO NOTHING;
	`); err != nil {
		t.Fatalf("seed capability aliases: %v", err)
	}

	agent := uuidByte(171)
	agentAll := uuidByte(172)
	agentDisabled := uuidByte(173)
	const tool = "replay.role_tool"
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent (id, workspace_id, name, runtime_mode)
		VALUES ($1, $4, 'Migration replay agent', 'local'),
		       ($2, $4, 'Migration replay all-sources agent', 'local'),
		       ($3, $4, 'Migration replay disabled agent', 'local')
	`, agent, agentAll, agentDisabled, tpTestWorkspaceID); err != nil {
		t.Fatalf("create migration replay agent: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM cerebro_role_assignment
		WHERE role_id IN (
			SELECT id FROM cerebro_role
			WHERE workspace_id = $1 AND name = 'Migrated agent ' || $2::text
		)
	`, tpTestWorkspaceID, util.UUIDToString(agent)); err != nil {
		t.Fatalf("clear pre-existing migrated role assignments: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM cerebro_role
		WHERE workspace_id = $1 AND name = 'Migrated agent ' || $2::text
	`, tpTestWorkspaceID, util.UUIDToString(agent)); err != nil {
		t.Fatalf("clear pre-existing migrated role fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_tool_grant (agent_id, tool_name, enabled, config_json)
		VALUES ($1, 'legacy.allowed_alias', true, NULL),
		       ($1, 'legacy.denied_tool', false, NULL),
		       ($1, 'legacy.configured_tool', true, '{"mode":"restricted","scope":["one"]}'::jsonb),
		       ($1, 'firtal_registry', true, '{"allowed_data_sources":["source-b","source-a"],"allowed_apps":true,"allow_write":false}'::jsonb),
		       ($2, 'firtal_registry', true, '{"allowed_data_sources_all":true,"allowed_data_sources":[],"allowed_apps":false,"allow_write":true}'::jsonb),
		       ($3, 'firtal_registry', false, '{"allowed_data_sources_all":true,"allowed_apps":true,"allow_write":true}'::jsonb)
	`, agent, agentAll, agentDisabled); err != nil {
		t.Fatalf("seed legacy direct grants: %v", err)
	}
	// Two agent-layer rows for the SAME tool that differ on resource_pattern —
	// the shape the buggy jsonb_object_agg collapsed to one arbitrary winner.
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, setting, resource_pattern)
		VALUES ($1, $2, 'agent', $3, 'allow', 'action:list_apps'),
		       ($1, $2, 'agent', $3, 'deny', '')
	`, tpTestWorkspaceID, tool, agent); err != nil {
		t.Fatalf("seed agent policy rows: %v", err)
	}

	replayMigration(t, ctx, tx, "9152_cerebro_roles_task_mandates.up.sql")

	var permissions []byte
	if err := tx.QueryRow(ctx, `
		SELECT permissions -> $3 FROM cerebro_role
		WHERE workspace_id = $1 AND name = 'Migrated agent ' || $2::text
	`, tpTestWorkspaceID, util.UUIDToString(agent), tool).Scan(&permissions); err != nil {
		t.Fatalf("read packaged role: %v", err)
	}
	rules, err := decodeRolePermission(bytes.TrimSpace(permissions))
	if err != nil {
		t.Fatalf("decode packaged permission: %v (raw %s)", err, permissions)
	}
	if len(rules) != 2 {
		t.Fatalf("packaged rules = %d (raw %s), want 2 — rules for one tool must not collapse", len(rules), permissions)
	}
	byPattern := map[string]string{}
	for _, r := range rules {
		byPattern[r.ResourcePattern] = r.Setting
	}
	if byPattern["action:list_apps"] != "allow" || byPattern[""] != "deny" {
		t.Fatalf("packaged rules = %v, want scoped allow + capability-wide deny", byPattern)
	}

	for legacyTool, wantSetting := range map[string]string{
		"legacy.allowed_tool":    "allow",
		"legacy.denied_tool":     "deny",
		"legacy.configured_tool": "allow",
	} {
		var raw []byte
		if err := tx.QueryRow(ctx, `
			SELECT permissions -> $3 FROM cerebro_role
			WHERE workspace_id = $1 AND name = 'Migrated agent ' || $2::text
		`, tpTestWorkspaceID, util.UUIDToString(agent), legacyTool).Scan(&raw); err != nil {
			t.Fatalf("read migrated legacy tool %s: %v", legacyTool, err)
		}
		legacyRules, err := decodeRolePermission(bytes.TrimSpace(raw))
		if err != nil || len(legacyRules) != 1 || legacyRules[0].Setting != wantSetting || legacyRules[0].ResourcePattern != "" {
			var allPermissions []byte
			_ = tx.QueryRow(ctx, `
				SELECT permissions FROM cerebro_role
				WHERE workspace_id = $1 AND name = 'Migrated agent ' || $2::text
			`, tpTestWorkspaceID, util.UUIDToString(agent)).Scan(&allPermissions)
			t.Fatalf("legacy tool %s raw = %s rules = %+v (decode err %v), want one capability-wide %s; all permissions = %s", legacyTool, raw, legacyRules, err, wantSetting, allPermissions)
		}
	}

	var archivedAlias string
	if err := tx.QueryRow(ctx, `
		SELECT tool_name
		FROM cerebro_legacy_agent_tool_grant_archive
		WHERE workspace_id = $1 AND agent_id = $2 AND tool_name = 'legacy.allowed_alias'
	`, tpTestWorkspaceID, agent).Scan(&archivedAlias); err != nil {
		t.Fatalf("read archived alias: %v", err)
	}
	if archivedAlias != "legacy.allowed_alias" {
		t.Fatalf("archive lost observed alias: %q", archivedAlias)
	}

	var registryRaw []byte
	if err := tx.QueryRow(ctx, `
		SELECT permissions -> 'firtal_registry' FROM cerebro_role
		WHERE workspace_id = $1 AND name = 'Migrated agent ' || $2::text
	`, tpTestWorkspaceID, util.UUIDToString(agent)).Scan(&registryRaw); err != nil {
		t.Fatalf("read migrated Registry rules: %v", err)
	}
	registryRules, err := decodeRolePermission(bytes.TrimSpace(registryRaw))
	if err != nil {
		t.Fatalf("decode Registry rules: %v", err)
	}
	registryByPattern := map[string]string{}
	var registryDataSourceCondition *Condition
	for _, rule := range registryRules {
		registryByPattern[rule.ResourcePattern] = rule.Setting
		if rule.ResourcePattern == "" {
			registryDataSourceCondition = rule.Conditions
		}
	}
	if registryByPattern[""] != "allow" || registryByPattern["action:list_apps"] != "allow" || registryByPattern["action:update_app"] != "deny" {
		t.Fatalf("Registry rules = %v, want generic allow + preserved action allow/deny", registryByPattern)
	}
	if registryDataSourceCondition == nil || len(registryDataSourceCondition.ArgAllowlist) != 1 {
		t.Fatalf("Registry data-source condition = %+v, want one data_source_id allowlist", registryDataSourceCondition)
	}

	assertRegistryCapability := func(subject pgtype.UUID, wantSetting string, wantConditions bool) {
		t.Helper()
		var raw []byte
		if err := tx.QueryRow(ctx, `
			SELECT permissions -> 'firtal_registry'
			FROM cerebro_role
			WHERE workspace_id = $1 AND name = 'Migrated agent ' || $2::text
		`, tpTestWorkspaceID, util.UUIDToString(subject)).Scan(&raw); err != nil {
			t.Fatalf("read Registry role for %s: %v", util.UUIDToString(subject), err)
		}
		rules, err := decodeRolePermission(bytes.TrimSpace(raw))
		if err != nil {
			t.Fatalf("decode Registry role for %s: %v", util.UUIDToString(subject), err)
		}
		for _, rule := range rules {
			if rule.ResourcePattern == "" {
				if rule.Setting != wantSetting {
					t.Fatalf("Registry setting for %s = %s, want %s", util.UUIDToString(subject), rule.Setting, wantSetting)
				}
				if (rule.Conditions != nil) != wantConditions {
					t.Fatalf("Registry conditions for %s = %+v, want present=%v", util.UUIDToString(subject), rule.Conditions, wantConditions)
				}
				return
			}
		}
		t.Fatalf("Registry capability-wide rule missing for %s: %+v", util.UUIDToString(subject), rules)
	}
	assertRegistryCapability(agentAll, "allow", false)
	assertRegistryCapability(agentDisabled, "deny", false)
	argAllow := registryDataSourceCondition.ArgAllowlist[0]
	if argAllow.Arg != "data_source_id" || !reflect.DeepEqual(argAllow.Values, []string{"source-b", "source-a"}) {
		t.Fatalf("Registry data-source allowlist = %+v, want the exact legacy source restriction", argAllow)
	}

	var archivedConfig []byte
	if err := tx.QueryRow(ctx, `
		SELECT config_json
		FROM cerebro_legacy_agent_tool_grant_archive
		WHERE workspace_id = $1 AND agent_id = $2 AND tool_name = 'legacy.configured_tool'
	`, tpTestWorkspaceID, agent).Scan(&archivedConfig); err != nil {
		t.Fatalf("read archived legacy config: %v", err)
	}
	var archived map[string]any
	if err := json.Unmarshal(archivedConfig, &archived); err != nil {
		t.Fatalf("decode archived legacy config: %v", err)
	}
	if archived["mode"] != "restricted" {
		t.Fatalf("archived config = %v, want complete legacy payload", archived)
	}

	collisionName := "Migration replay all-sources agent"
	collisionFallback := collisionName + " (" + util.UUIDToString(agentAll) + ")"
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_role (workspace_id, name)
		VALUES ($1, $2), ($1, $3)
	`, tpTestWorkspaceID, collisionName, collisionFallback); err != nil {
		t.Fatalf("seed permission profile name collisions: %v", err)
	}

	replayMigration(t, ctx, tx, "9167_cerebro_permission_profile_names.up.sql")
	var profileName, profileDescription string
	if err := tx.QueryRow(ctx, `
		SELECT name, description FROM cerebro_role
		WHERE workspace_id = $1 AND id IN (
			SELECT role_id FROM cerebro_role_assignment
			WHERE subject_type = 'agent' AND subject_id = $2
		)
	`, tpTestWorkspaceID, agent).Scan(&profileName, &profileDescription); err != nil {
		t.Fatalf("read human-named permission profile: %v", err)
	}
	if profileName != "Migration replay agent" || profileDescription != "Keeps the permissions that were previously configured directly for Migration replay agent." {
		t.Fatalf("permission profile = %q / %q, want the assigned agent's readable name and description", profileName, profileDescription)
	}

	if err := tx.QueryRow(ctx, `
		SELECT name, description FROM cerebro_role
		WHERE workspace_id = $1 AND id IN (
			SELECT role_id FROM cerebro_role_assignment
			WHERE subject_type = 'agent' AND subject_id = $2
		)
	`, tpTestWorkspaceID, agentAll).Scan(&profileName, &profileDescription); err != nil {
		t.Fatalf("read collision-safe permission profile: %v", err)
	}
	wantCollisionName := "Migrated agent " + util.UUIDToString(agentAll)
	if profileName != wantCollisionName || profileDescription != "Automatically migrated from direct agent policy rows" {
		t.Fatalf("collision-safe permission profile = %q / %q, want the unchanged migration profile", profileName, profileDescription)
	}
}
