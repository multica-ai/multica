package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const maxLegacyMigrationPrefix = 148
const concurrentIndexMigrationFloor = 267

var legacyDuplicateMigrationStems = map[string][]string{
	"020": {"020_issue_number", "020_task_session"},
	"026": {"026_comment_reactions", "026_task_messages"},
	"029": {"029_attachment", "029_daemon_token", "029_drop_daemon_pairing"},
	"032": {"032_drop_agent_triggers", "032_issue_search_index", "032_runtime_owner", "032_task_usage"},
	"033": {"033_chat", "033_comment_search_index"},
	"035": {"035_project_priority", "035_task_queue_issue_id_index"},
	"040": {"040_agent_custom_env", "040_chat_unread_since"},
	"041": {"041_agent_custom_args", "041_workspace_invitation"},
	"043": {"043_audit_reserved_slugs", "043_fix_orphaned_autopilot_runs"},
	"046": {"046_agent_mcp_config", "046_agent_unique_name", "046_drop_runtime_usage"},
	"050": {"050_add_onboarded_at_to_users", "050_agent_model", "050_issue_first_executed_at"},
	"060": {"060_add_user_language", "060_agent_description_length", "060_chat_session_runtime_id", "060_issue_origin_quick_create"},
	"065": {"065_backfill_onboarded_at", "065_project_resources"},
	"069": {"069_comment_resolved_at", "069_drop_task_last_heartbeat"},
	"079": {"079_autopilot_run_skipped_status", "079_backfill_api_invalid_request", "079_github_integration"},
	"083": {"083_attachment_chat_columns", "083_runtime_visibility"},
	"084": {"084_squad", "084_task_usage_dashboard_rollup"},
	"091": {"091_autopilot_webhook_triggers", "091_issue_start_date", "091_pr_ci_conflict"},
	"095": {"095_agent_thinking_level", "095_backfill_starter_content_state"},
	"096": {"096_autopilot_squad_assignee", "096_pending_check_suite", "096_user_profile_description"},
	"098": {"098_contact_sales_inquiries", "098_user_onboarding_runtime_choice"},
	"109": {"109_agent_task_waiting_local_directory", "109_drop_agent_skills_local", "109_issue_pull_request_close_intent", "109_lark_integration"},
	"111": {"111_issue_origin_lark_chat", "111_workspace_avatar"},
	"112": {"112_issue_dates_to_date", "112_lark_installation_bot_union_id"},
	"113": {"113_lark_inbound_dedup_per_installation", "113_sys_cron_executions"},
	"120": {"120_autopilot_subscriber", "120_comment_source_task_id", "120_github_pending_installation", "120_runtime_profile"},
	"122": {"122_lark_chat_session_binding_thread_reply", "122_task_handoff_note"},
	"124": {"124_autopilot_run_planned_at", "124_channel_generalization", "124_task_prepare_lease"},
	"127": {"127_issue_pull_request_reference_only", "127_task_squad_id", "127_user_composio_connection"},
	"128": {"128_agent_task_queue_runtime_mcp_overlay", "128_autopilot_collaborator", "128_comment_routing_escalation"},
}

var migrationPrefixPattern = regexp.MustCompile(`^(\d+)_`)

func TestMigrationFilesHaveMatchingDirections(t *testing.T) {
	files := migrationFilesForLint(t, "*.sql")

	directionsByStem := make(map[string]map[string]bool)
	for _, file := range files {
		stem, direction, ok := splitMigrationFilename(filepath.Base(file))
		if !ok {
			continue
		}
		if directionsByStem[stem] == nil {
			directionsByStem[stem] = make(map[string]bool)
		}
		directionsByStem[stem][direction] = true
	}

	for stem, directions := range directionsByStem {
		if !directions["up"] || !directions["down"] {
			t.Errorf("migration %s must have both .up.sql and .down.sql files", stem)
		}
	}
}

func TestMigrationNumericPrefixesStayUniqueAfterLegacySet(t *testing.T) {
	stemsByPrefix := migrationStemsByPrefix(t)

	for prefix, stems := range stemsByPrefix {
		sort.Strings(stems)

		legacyStems, isLegacyDuplicate := legacyDuplicateMigrationStems[prefix]
		if isLegacyDuplicate {
			expected := append([]string(nil), legacyStems...)
			sort.Strings(expected)
			if !reflect.DeepEqual(stems, expected) {
				t.Errorf("legacy duplicate migration prefix %s changed: got %v, want %v; do not add to or rename historical duplicate-prefix migrations", prefix, stems, expected)
			}
			continue
		}

		if len(stems) > 1 {
			t.Errorf("migration prefix %s is reused by %v; use the next unique prefix instead", prefix, stems)
		}
	}
}

func TestNewMigrationPrefixesStartAfterLegacyRange(t *testing.T) {
	stemsByPrefix := migrationStemsByPrefix(t)

	for prefix, stems := range stemsByPrefix {
		n, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatalf("parse migration prefix %q: %v", prefix, err)
		}
		if n <= maxLegacyMigrationPrefix && !isKnownLegacyPrefix(prefix) {
			t.Errorf("migration prefix %s is in the frozen legacy range 001-%03d: %v; new migrations must start at %03d", prefix, maxLegacyMigrationPrefix, stems, maxLegacyMigrationPrefix+1)
		}
	}
}

func TestRuntimePoolEraIndexMigrationsStayConcurrentAndSingleStatement(t *testing.T) {
	for _, file := range migrationFilesForLint(t, "*.sql") {
		base := filepath.Base(file)
		match := migrationPrefixPattern.FindStringSubmatch(base)
		if match == nil {
			continue
		}
		prefix, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse migration prefix for %s: %v", base, err)
		}
		if prefix < concurrentIndexMigrationFloor {
			continue
		}

		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		statements := sqlStatementsForLint(string(raw))
		for _, statement := range statements {
			upper := strings.ToUpper(statement)
			if !strings.HasPrefix(upper, "CREATE INDEX ") &&
				!strings.HasPrefix(upper, "CREATE UNIQUE INDEX ") &&
				!strings.HasPrefix(upper, "DROP INDEX ") {
				continue
			}
			if !strings.Contains(upper, " INDEX CONCURRENTLY ") {
				t.Errorf("%s builds or drops a physical index without CONCURRENTLY", base)
			}
			if len(statements) != 1 {
				t.Errorf("%s contains a physical index operation plus %d total statements; keep the index operation alone", base, len(statements))
			}
		}
	}
}

func TestRuntimePoolIndexSwapsBuildReplacementBeforeDroppingPreviousVersion(t *testing.T) {
	dir := realMigrationsDir(t)
	testCases := []struct {
		name            string
		buildFile       string
		dropFile        string
		restoreFile     string
		replacementName string
		previousName    string
	}{
		{
			name:            "issue_pending_v3_over_v2",
			buildFile:       "272_pending_issue_agent_pool_v3.up.sql",
			dropFile:        "273_drop_pending_issue_agent_v2.up.sql",
			restoreFile:     "273_drop_pending_issue_agent_v2.down.sql",
			replacementName: "idx_one_pending_task_per_issue_agent_v3",
			previousName:    "idx_one_pending_task_per_issue_agent_v2",
		},
		{
			name:            "chat_pending_v4_over_v3",
			buildFile:       "274_chat_pending_pool_v4.up.sql",
			dropFile:        "275_drop_chat_pending_v3.up.sql",
			restoreFile:     "275_drop_chat_pending_v3.down.sql",
			replacementName: "idx_agent_task_queue_chat_pending_v4",
			previousName:    "idx_agent_task_queue_chat_pending_v3",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buildPrefix := migrationPrefixFromFilename(t, tc.buildFile)
			dropPrefix := migrationPrefixFromFilename(t, tc.dropFile)
			if buildPrefix >= dropPrefix {
				t.Fatalf("replacement build prefix %d must precede old-index drop prefix %d", buildPrefix, dropPrefix)
			}

			build := readMigrationForLint(t, filepath.Join(dir, tc.buildFile))
			if !strings.Contains(build, "CREATE") || !strings.Contains(build, "INDEX CONCURRENTLY IF NOT EXISTS "+strings.ToUpper(tc.replacementName)) {
				t.Fatalf("%s does not concurrently build %s", tc.buildFile, tc.replacementName)
			}
			drop := readMigrationForLint(t, filepath.Join(dir, tc.dropFile))
			if !strings.Contains(drop, "DROP INDEX CONCURRENTLY "+strings.ToUpper(tc.previousName)) {
				t.Fatalf("%s does not concurrently drop %s", tc.dropFile, tc.previousName)
			}
			restore := readMigrationForLint(t, filepath.Join(dir, tc.restoreFile))
			if !strings.Contains(restore, "CREATE") || !strings.Contains(restore, "INDEX CONCURRENTLY "+strings.ToUpper(tc.previousName)) {
				t.Fatalf("%s does not restore %s before reverse migration removes the replacement", tc.restoreFile, tc.previousName)
			}
			if strings.Contains(restore, "IF NOT EXISTS") {
				t.Fatalf("%s must fail closed on a same-name INVALID leftover", tc.restoreFile)
			}
		})
	}
}

func migrationStemsByPrefix(t *testing.T) map[string][]string {
	t.Helper()

	files := migrationFilesForLint(t, "*.up.sql")
	stemsByPrefix := make(map[string][]string)
	for _, file := range files {
		stem := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		match := migrationPrefixPattern.FindStringSubmatch(stem)
		if match == nil {
			t.Fatalf("migration %s does not start with a numeric prefix followed by underscore", stem)
		}
		stemsByPrefix[match[1]] = append(stemsByPrefix[match[1]], stem)
	}
	return stemsByPrefix
}

func migrationPrefixFromFilename(t *testing.T, name string) int {
	t.Helper()
	match := migrationPrefixPattern.FindStringSubmatch(name)
	if match == nil {
		t.Fatalf("migration %s has no numeric prefix", name)
	}
	prefix, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("parse migration prefix for %s: %v", name, err)
	}
	return prefix
}

func readMigrationForLint(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	statements := sqlStatementsForLint(string(raw))
	if len(statements) != 1 {
		t.Fatalf("%s has %d top-level statements, want 1", filepath.Base(path), len(statements))
	}
	return strings.ToUpper(strings.Join(strings.Fields(statements[0]), " "))
}

func migrationFilesForLint(t *testing.T, pattern string) []string {
	t.Helper()

	dir := realMigrationsDir(t)
	files, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no migration files matched %s in %s", pattern, dir)
	}
	sort.Strings(files)
	return files
}

func realMigrationsDir(t *testing.T) string {
	t.Helper()

	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migration lint test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(self), "..", "..", "migrations"))
}

func splitMigrationFilename(name string) (stem, direction string, ok bool) {
	for _, candidateDirection := range []string{"up", "down"} {
		suffix := fmt.Sprintf(".%s.sql", candidateDirection)
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix), candidateDirection, true
		}
	}
	return "", "", false
}

func isKnownLegacyPrefix(prefix string) bool {
	if _, ok := legacyDuplicateMigrationStems[prefix]; ok {
		return true
	}

	switch prefix {
	case "001", "002", "003", "004", "005", "006", "007", "008", "009", "010",
		"011", "012", "013", "014", "015", "016", "017", "018", "019", "021",
		"022", "023", "024", "025", "027", "028", "030", "031", "034", "036",
		"037", "038", "039", "042", "044", "045", "047", "048", "049", "051",
		"052", "053", "054", "055", "056", "057", "058", "059", "061", "062",
		"063", "064", "066", "067", "068", "072", "073", "074", "075", "076",
		"077", "078", "080", "081", "082", "085", "086", "087", "088", "089",
		"090", "092", "093", "094", "097", "100", "101", "102", "103", "104",
		"105", "106", "107", "108", "110", "114", "115", "116", "117", "118",
		"119", "121", "123", "125", "126", "129", "130", "131", "132", "133",
		"134", "135", "136", "137", "138", "139", "140", "141", "142", "143",
		"144", "145", "146", "147", "148":
		return true
	default:
		return false
	}
}

func sqlStatementsForLint(raw string) []string {
	lines := strings.Split(raw, "\n")
	withoutComments := make([]string, 0, len(lines))
	for _, line := range lines {
		if index := strings.Index(line, "--"); index >= 0 {
			line = line[:index]
		}
		withoutComments = append(withoutComments, line)
	}
	parts := strings.Split(strings.Join(withoutComments, "\n"), ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		if statement := strings.TrimSpace(part); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}
