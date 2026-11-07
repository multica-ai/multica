package handler

import (
	"context"
	"sort"
	"testing"
)

// Move history has no live workspace ownership column and is retained.
// Register only these new tables in the existing teardown coverage contract;
// no ordinary teardown query or handler changes.
func init() {
	workspaceDeletionManifest["issue_key_alias"] = workspaceDeleteKeep
	workspaceDeletionManifest["project_move"] = workspaceDeleteKeep
}

// Conditional move selectors are implemented in the application graph.
var projectMoveManifest = map[string]string{
	"wecom_install_session":              "n-a",
	"skill_version":                      "n-a",
	"member_delegation_day":              "n-a",
	"issue_workflow":                     "n-a",
	"issue_unread":                       "delete",
	"cloud_agent_connection":             "n-a",
	"activity_log":                       "rehome",
	"agent":                              "n-a",
	"agent_builder_draft":                "n-a",
	"agent_invocation_target":            "n-a",
	"agent_mcp_server":                   "n-a",
	"agent_runtime":                      "n-a",
	"agent_skill":                        "n-a",
	"agent_task_queue":                   "block",
	"agent_to_label":                     "n-a",
	"attachment":                         "rehome",
	"autopilot":                          "block",
	"autopilot_collaborator":             "n-a",
	"autopilot_quota_period":             "n-a",
	"autopilot_quota_reservation":        "n-a",
	"autopilot_rule_version":             "n-a",
	"autopilot_run":                      "n-a",
	"autopilot_subscriber":               "n-a",
	"autopilot_trigger":                  "n-a",
	"channel_binding_token":              "n-a",
	"channel_chat_context_generation":    "n-a",
	"channel_chat_session_binding":       "n-a",
	"channel_inbound_audit":              "n-a",
	"channel_inbound_message_dedup":      "n-a",
	"channel_installation":               "n-a",
	"channel_media_pending_object":       "n-a",
	"channel_outbound_card_message":      "n-a",
	"channel_outbound_message":           "n-a",
	"channel_reply_delivery":             "n-a",
	"channel_task_delivery":              "n-a",
	"channel_typing_reaction":            "n-a",
	"channel_user_binding":               "n-a",
	"chat_draft_restore":                 "n-a",
	"chat_message":                       "n-a",
	"chat_pinned_agent":                  "n-a",
	"chat_session":                       "keep",
	"client_usage_daily":                 "n-a",
	"comment":                            "rehome",
	"comment_reaction":                   "rehome",
	"contact_sales_inquiry":              "n-a",
	"daemon_connection":                  "n-a",
	"daemon_token":                       "n-a",
	"dingtalk_bot_identity":              "n-a",
	"dingtalk_group_presence":            "n-a",
	"dingtalk_group_route":               "n-a",
	"feedback":                           "n-a",
	"github_installation":                "n-a",
	"github_pending_check_suite":         "n-a",
	"github_pending_installation":        "n-a",
	"github_pull_request":                "n-a",
	"github_pull_request_check_run":      "n-a",
	"github_pull_request_check_suite":    "n-a",
	"inbox_item":                         "delete",
	"instance_telemetry_state":           "n-a",
	"issue":                              "rehome",
	"issue_child_event":                  "rehome",
	"issue_dependency":                   "block",
	"issue_key_alias":                    "keep",
	"issue_label":                        "n-a",
	"issue_pr_automation":                "rehome",
	"issue_property":                     "n-a",
	"issue_pull_request":                 "delete",
	"issue_pull_request_exclusion":       "rehome",
	"issue_reaction":                     "rehome",
	"issue_source_context":               "rehome",
	"issue_source_context_object_intent": "rehome",
	"issue_status":                       "n-a",
	"issue_subscriber":                   "delete",
	"issue_to_label":                     "keep",
	"issue_vcs_pull_request":             "delete",
	"issue_view":                         "delete",
	"issue_view_preference":              "delete",
	"issue_wakeup":                       "block",
	"issue_wakeup_receipt":               "delete",
	"lark_binding_token":                 "n-a",
	"lark_chat_session_binding":          "n-a",
	"lark_inbound_audit":                 "n-a",
	"lark_inbound_message_dedup":         "n-a",
	"lark_installation":                  "n-a",
	"lark_outbound_card_message":         "n-a",
	"lark_user_binding":                  "n-a",
	"maintenance_job":                    "n-a",
	"member":                             "n-a",
	"notification_preference":            "n-a",
	"personal_access_token":              "n-a",
	"pinned_item":                        "delete",
	"plugin_hook_schedule":               "n-a",
	"plugin_installation":                "n-a",
	"plugin_invocation":                  "n-a",
	"plugin_package":                     "n-a",
	"plugin_package_file":                "n-a",
	"plugin_package_version":             "n-a",
	"plugin_secret":                      "n-a",
	"plugin_storage":                     "n-a",
	"project":                            "rehome",
	"project_move":                       "keep",
	"project_resource":                   "rehome",
	"quick_action":                       "n-a",
	"runtime_profile":                    "n-a",
	"schema_migrations":                  "n-a",
	"search_index_change":                "keep",
	"search_index_prune_mark":            "n-a",
	"seat_capacity_outbox":               "n-a",
	"skill":                              "n-a",
	"skill_file":                         "n-a",
	"skill_to_label":                     "n-a",
	"squad":                              "n-a",
	"squad_member":                       "n-a",
	"sys_cron_executions":                "n-a",
	"task_message":                       "keep",
	"task_supplement":                    "rehome",
	"task_supplement_capability":         "rehome",
	"task_token":                         "keep",
	"task_usage":                         "keep",
	"task_usage_hourly":                  "keep",
	"task_usage_hourly_dirty":            "keep",
	"task_usage_hourly_rollup_state":     "n-a",
	"user":                               "n-a",
	"user_composio_connection":           "n-a",
	"vcs_commit_status":                  "n-a",
	"vcs_connection":                     "n-a",
	"vcs_pull_request":                   "n-a",
	"verification_code":                  "n-a",
	"webhook_delivery":                   "n-a",
	"workspace":                          "n-a",
	"workspace_invitation":               "n-a",
	"workspace_mcp_server":               "n-a",
	"workspace_share_link":               "n-a",
}

func TestProjectMoveManifestCoversPublicSchema(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	rows, err := testPool.Query(context.Background(), `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actual := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		actual[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var missing, stale []string
	for name := range actual {
		if _, ok := projectMoveManifest[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name, action := range projectMoveManifest {
		if !actual[name] {
			stale = append(stale, name)
		}
		switch action {
		case "rehome", "delete", "keep", "block", "n-a":
		default:
			t.Errorf("invalid move action %s: %s", name, action)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing)+len(stale) > 0 {
		t.Fatalf("move manifest drift: unclassified=%v missing=%v", missing, stale)
	}
	for _, table := range []string{"issue_key_alias", "project_move"} {
		if !actual[table] {
			t.Errorf("missing move table %s", table)
		}
	}
}
