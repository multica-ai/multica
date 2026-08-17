package migrations

import (
	"fmt"
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
