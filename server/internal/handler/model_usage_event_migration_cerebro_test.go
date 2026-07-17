package handler

import (
	"os"
	"strings"
	"testing"
)

// CEREBRO-PATCH(model-usage-event-ledger-schema-test): FIR-3337 locks the
// canonical call-level ledger and its attribution columns before ingestion.
func TestModelUsageEventMigrationDefinesCanonicalLedger(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/9143_model_usage_event_ledger.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	for _, contract := range []string{
		"CREATE TABLE IF NOT EXISTS model_usage_event",
		"event_id TEXT NOT NULL",
		"task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE",
		"workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE",
		"issue_id UUID REFERENCES issue(id) ON DELETE CASCADE",
		"agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE",
		"runtime_id UUID REFERENCES agent_runtime(id) ON DELETE SET NULL",
		"session_root_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL",
		"chat_session_id UUID REFERENCES chat_session(id) ON DELETE SET NULL",
		"autopilot_run_id UUID REFERENCES autopilot_run(id) ON DELETE SET NULL",
		"provider_session_id TEXT",
		"call_id TEXT",
		"context_window_tokens BIGINT NOT NULL DEFAULT 0",
		"'reconciliation'",
		"UNIQUE (task_id, event_id)",
		"idx_model_usage_event_call_identity",
		"idx_model_usage_event_session_observed",
		"idx_model_usage_event_issue_observed",
		"idx_model_usage_event_provider_session",
	} {
		if !strings.Contains(schema, contract) {
			t.Fatalf("model usage event migration is missing %q", contract)
		}
	}
}

// CEREBRO-PATCH(model-usage-task-rollup-schema-test): FIR-3337 keeps existing
// consumer contracts while canonical events replace task_usage per task.
func TestModelUsageTaskRollupMigrationPrefersCanonicalEventsPerTask(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/9144_model_usage_task_rollup.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	for _, contract := range []string{
		"CREATE OR REPLACE VIEW model_usage_task_rollup AS",
		"FROM model_usage_event",
		"SUM(output_tokens + reasoning_tokens)",
		"FROM task_usage tu",
		"NOT EXISTS",
		"fallback.task_id IS NOT NULL",
	} {
		if !strings.Contains(schema, contract) {
			t.Fatalf("model usage task rollup migration is missing %q", contract)
		}
	}
}

// CEREBRO-PATCH(model-usage-consumer-cutover-test): FIR-3337 prevents active
// API consumers from silently returning to the shadow task_usage table.
func TestModelUsageConsumersReadCompatibilityRollup(t *testing.T) {
	files := []string{
		"../../pkg/db/queries/task_usage.sql",
		"../../pkg/db/queries/runtime_usage.sql",
		"../cerebro/queries/agent_pass.sql",
		"../cerebro/queries/chat_message_cost.sql",
		"../cerebro/queries/comment_cost.sql",
		"../cerebro/queries/dashboard.sql",
		"../cerebro/queries/tasks.sql",
		"../cerebro/analytics/postgres.go",
		"dashboard_usage_explorer_cerebro.go",
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		for _, legacyRead := range []string{"FROM task_usage ", "JOIN task_usage "} {
			if strings.Contains(source, legacyRead) {
				t.Errorf("%s still contains legacy read %q", file, legacyRead)
			}
		}
	}
}
