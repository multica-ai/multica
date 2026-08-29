package migrations

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	operationalControlsFirstPrefix = 442
	operationalControlsLastPrefix  = 480
)

var operationalControlsMigrationStems = []string{
	"442_agent_operating_mode",
	"443_agent_tool_policy",
	"444_agent_tool_policy_revision",
	"445_agent_tool_policy_rule",
	"446_agent_tool_approval_request",
	"447_agent_tool_action_event",
	"448_agent_tool_policy_pkey_index",
	"449_agent_tool_policy_primary_key",
	"450_agent_tool_policy_agent_unique_index",
	"451_agent_tool_policy_agent_unique",
	"452_agent_tool_policy_workspace_agent_index",
	"453_agent_tool_policy_revision_pkey_index",
	"454_agent_tool_policy_revision_primary_key",
	"455_agent_tool_policy_revision_agent_revision_unique_index",
	"456_agent_tool_policy_revision_agent_revision_unique",
	"457_agent_tool_policy_revision_workspace_agent_index",
	"458_agent_tool_policy_rule_pkey_index",
	"459_agent_tool_policy_rule_primary_key",
	"460_agent_tool_policy_rule_identity_unique_index",
	"461_agent_tool_policy_rule_identity_unique",
	"462_agent_tool_policy_rule_workspace_agent_index",
	"463_agent_tool_policy_rule_policy_index",
	"464_agent_tool_approval_request_pkey_index",
	"465_agent_tool_approval_request_primary_key",
	"466_agent_tool_approval_request_task_idempotency_unique_index",
	"467_agent_tool_approval_request_task_idempotency_unique",
	"468_agent_tool_approval_request_task_invocation_unique_index",
	"469_agent_tool_approval_request_task_invocation_unique",
	"470_agent_tool_approval_request_pending_queue_index",
	"471_agent_tool_approval_request_agent_history_index",
	"472_agent_tool_approval_request_expiry_index",
	"473_agent_tool_approval_request_retention_index",
	"474_agent_tool_action_event_pkey_index",
	"475_agent_tool_action_event_primary_key",
	"476_agent_tool_action_event_identity_unique_index",
	"477_agent_tool_action_event_identity_unique",
	"478_agent_tool_action_event_agent_history_index",
	"479_agent_tool_action_event_retention_index",
	"480_agent_tool_action_event_dashboard_index",
}

func TestOperationalControlsMigrationBlockIsContiguousAndComplete(t *testing.T) {
	if len(operationalControlsMigrationStems) != operationalControlsLastPrefix-operationalControlsFirstPrefix+1 {
		t.Fatalf("migration plan has %d stems, want %d", len(operationalControlsMigrationStems), operationalControlsLastPrefix-operationalControlsFirstPrefix+1)
	}

	for offset, stem := range operationalControlsMigrationStems {
		wantPrefix := operationalControlsFirstPrefix + offset
		prefixText := strings.SplitN(stem, "_", 2)[0]
		prefix, err := strconv.Atoi(prefixText)
		if err != nil {
			t.Fatalf("parse migration stem %q: %v", stem, err)
		}
		if prefix != wantPrefix {
			t.Errorf("migration stem %q has prefix %d, want %d", stem, prefix, wantPrefix)
		}
		for _, direction := range []string{"up", "down"} {
			path := filepath.Join(realMigrationsDir(t), stem+"."+direction+".sql")
			if _, err := os.Stat(path); err != nil {
				t.Errorf("required migration %s: %v", filepath.Base(path), err)
			}
		}
	}
}

func TestOperationalControlsMigrationsEnforceSchemaSafety(t *testing.T) {
	for _, stem := range operationalControlsMigrationStems {
		for _, direction := range []string{"up", "down"} {
			name := stem + "." + direction + ".sql"
			body := readMigrationFile(t, name)
			upper := strings.ToUpper(stripSQLComments(body))

			for _, forbidden := range []string{"FOREIGN KEY", "REFERENCES", " ON DELETE ", " ON UPDATE ", " CASCADE"} {
				if strings.Contains(upper, forbidden) {
					t.Errorf("%s contains forbidden database relationship token %q", name, strings.TrimSpace(forbidden))
				}
			}

			if direction == "up" && strings.Contains(upper, "CREATE TABLE") {
				if regexp.MustCompile(`\bPRIMARY\s+KEY\b`).MatchString(upper) {
					t.Errorf("%s creates an inline primary key", name)
				}
				if regexp.MustCompile(`\bUNIQUE\s*\(`).MatchString(upper) {
					t.Errorf("%s creates an inline unique index", name)
				}
			}

			if direction == "up" && strings.Contains(upper, "CREATE INDEX") || direction == "up" && strings.Contains(upper, "CREATE UNIQUE INDEX") {
				if !strings.Contains(upper, "INDEX CONCURRENTLY") {
					t.Errorf("%s creates an index without CONCURRENTLY", name)
				}
				if statementCount(body) != 1 {
					t.Errorf("%s contains %d SQL statements, want one", name, statementCount(body))
				}
			}

			if direction == "up" && strings.Contains(upper, " USING INDEX ") {
				if statementCount(body) != 1 || !strings.HasPrefix(strings.TrimSpace(upper), "ALTER TABLE") {
					t.Errorf("%s must attach one constraint in one ALTER TABLE statement", name)
				}
			}
		}
	}
}

func TestOperatingModeMigrationLocksDefaultAndVocabulary(t *testing.T) {
	body := strings.ToLower(readMigrationFile(t, "442_agent_operating_mode.up.sql"))
	for _, required := range []string{
		"operating_mode text not null default 'coding'",
		"operating_mode in ('coding', 'operational', 'hybrid')",
	} {
		if !strings.Contains(compactWhitespace(body), required) {
			t.Errorf("operating mode migration missing %q", required)
		}
	}
}

func TestAgentQueriesUseTypedOperatingModeParameters(t *testing.T) {
	queryPath := filepath.Join(realMigrationsDir(t), "..", "pkg", "db", "queries", "agent.sql")
	bodyBytes, err := os.ReadFile(queryPath)
	if err != nil {
		t.Fatalf("read agent queries: %v", err)
	}
	body := compactWhitespace(strings.ToLower(string(bodyBytes)))
	for _, required := range []string{
		"coalesce(sqlc.narg('operating_mode')::text, 'coding')",
		"operating_mode = coalesce(sqlc.narg('operating_mode')::text, operating_mode)",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("agent queries missing typed operating mode contract %q", required)
		}
	}
}

func TestOperationalControlTablesCarryDirectWorkspaceScopeAndBoundedChecks(t *testing.T) {
	wantTables := map[string][]string{
		"443_agent_tool_policy.up.sql": {
			"workspace_id uuid not null", "status in ('draft', 'active')",
			"default_effect = 'deny'", "policy_digest ~ '^sha256:[0-9a-f]{64}$'",
		},
		"444_agent_tool_policy_revision.up.sql": {
			"workspace_id uuid not null", "rule_identities jsonb not null",
			"policy_digest ~ '^sha256:[0-9a-f]{64}$'",
		},
		"445_agent_tool_policy_rule.up.sql": {
			"workspace_id uuid not null", "effect in ('allow', 'require_approval')",
			"schema_digest ~ '^sha256:[0-9a-f]{64}$'",
		},
		"446_agent_tool_approval_request.up.sql": {
			"workspace_id uuid not null",
			"status in ('pending', 'approved', 'consumed', 'denied', 'expired', 'cancelled')",
			"expires_at <= requested_at + interval '24 hours'",
			"schema_digest ~ '^sha256:[0-9a-f]{64}$'",
		},
		"447_agent_tool_action_event.up.sql": {
			"workspace_id uuid not null",
			"coverage_kind in ('managed_mcp', 'managed_native', 'declaration_only')",
			"schema_digest ~ '^sha256:[0-9a-f]{64}$'",
		},
	}

	for name, fragments := range wantTables {
		body := compactWhitespace(strings.ToLower(readMigrationFile(t, name)))
		for _, fragment := range fragments {
			if !strings.Contains(body, fragment) {
				t.Errorf("%s missing schema contract %q", name, fragment)
			}
		}
	}
}

func TestOperationalControlsExactRuleIdentityAndRetentionIndex(t *testing.T) {
	ruleIdentityIndex := compactWhitespace(strings.ToLower(readMigrationFile(t, "460_agent_tool_policy_rule_identity_unique_index.up.sql")))
	if !strings.Contains(ruleIdentityIndex, "(agent_id, transport_kind, server_key, tool_name, schema_digest)") {
		t.Error("rule uniqueness must use the frozen exact identity including schema_digest")
	}

	ruleQueries := compactWhitespace(strings.ToLower(readQueryFile(
		t,
		filepath.Join(realMigrationsDir(t), "..", "pkg", "db", "queries"),
		"agent_tool_policy_rule.sql",
	)))
	if !strings.Contains(ruleQueries, "order by transport_kind asc, server_key asc, tool_name asc, schema_digest asc, id asc") {
		t.Error("exact rule listing must order by every identity component before id")
	}

	retentionIndex := compactWhitespace(strings.ToLower(readMigrationFile(t, "473_agent_tool_approval_request_retention_index.up.sql")))
	if !strings.Contains(retentionIndex, "(workspace_id, (coalesce(consumed_at, decided_at)) asc, id asc)") {
		t.Error("approval retention index must match the deterministic terminal timestamp ordering")
	}
}

func TestOperationalControlQueriesAreWorkspaceScopedAndDeterministic(t *testing.T) {
	queryDir := filepath.Join(realMigrationsDir(t), "..", "pkg", "db", "queries")
	queryFiles, err := filepath.Glob(filepath.Join(queryDir, "agent_tool_*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(queryFiles) < 4 {
		t.Fatalf("found %d operational-control query files, want at least four", len(queryFiles))
	}
	sort.Strings(queryFiles)

	queryPattern := regexp.MustCompile(`(?m)^-- name: ([A-Za-z0-9_]+) :[a-z]+\s*$`)
	for _, path := range queryFiles {
		bodyBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		body := string(bodyBytes)
		matches := queryPattern.FindAllStringSubmatchIndex(body, -1)
		if len(matches) == 0 {
			t.Errorf("%s contains no sqlc queries", filepath.Base(path))
			continue
		}
		for i, match := range matches {
			end := len(body)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			queryName := body[match[2]:match[3]]
			query := strings.ToLower(body[match[1]:end])
			if !strings.Contains(query, "workspace_id") {
				t.Errorf("%s query %s has no direct workspace_id scope", filepath.Base(path), queryName)
			}
		}
	}

	approvalBody := strings.ToLower(readQueryFile(t, queryDir, "agent_tool_approval.sql"))
	for _, required := range []string{
		"is not distinct from", "order by expires_at asc, id asc",
	} {
		if !strings.Contains(compactWhitespace(approvalBody), required) {
			t.Errorf("approval queries missing deterministic contract %q", required)
		}
	}

	actionBody := compactWhitespace(strings.ToLower(readQueryFile(t, queryDir, "agent_tool_action.sql")))
	if !strings.Contains(actionBody, "order by created_at desc, id desc") {
		t.Error("action cursor must order by created_at DESC, id DESC")
	}

	retentionBody := compactWhitespace(strings.ToLower(readQueryFile(t, queryDir, "agent_tool_retention.sql")))
	if !strings.Contains(retentionBody, "select 90::integer as retention_days") {
		t.Error("retention queries must expose the 90-day service default")
	}
	for _, required := range []string{
		"order by coalesce(approval.consumed_at, approval.decided_at) asc, approval.id asc",
		"order by event.created_at asc, event.id asc",
	} {
		if !strings.Contains(retentionBody, required) {
			t.Errorf("retention queries missing deterministic contract %q", required)
		}
	}
}

func readQueryFile(t *testing.T, dir, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read query file %s: %v", name, err)
	}
	return string(body)
}

func stripSQLComments(body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func statementCount(body string) int {
	return strings.Count(stripSQLComments(body), ";")
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
