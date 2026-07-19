package loops

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestChainCutoverMigrationConvertsLegacyRecipeWithoutLosingOrderedWork(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	tx, err := loopTestPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	issueID := seedIssue(t)
	var workspaceID string
	if err := tx.QueryRow(ctx, `SELECT workspace_id::text FROM issue WHERE id=$1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"planning":true,"plan_skill":"plan","build_agent_id":"00000000-0000-0000-0000-000000000001","build_skill":"build","caps":{"max_iterations":7,"max_revisions":4,"no_progress_stalls":2},"plan_gate":[{"id":"plan-review","type":"judge","rubric":"sound plan"}],"verification":[{"id":"tests","type":"programmatic","check":["go","test","./..."],"expect":"exit_zero"}],"done_status":"done"}`
	var recipeID string
	if err := tx.QueryRow(ctx, `INSERT INTO cerebro_workflow (workspace_id,name,trigger_type,trigger_config,conditions,action_type,action_config,editor_mode,editor_layout,created_by_id,created_by_type,workflow_type,loop_spec) VALUES ($1,'legacy cutover','status_changed','{}','[]','run_skill','{}','form','null',gen_random_uuid(),'member','issue_loop',$2) RETURNING id::text`, workspaceID, legacy).Scan(&recipeID); err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile("../../../migrations/9147_cerebro_issue_loop_chain_cutover.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("run cutover: %v", err)
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT loop_spec FROM cerebro_workflow WHERE id=$1`, recipeID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var chain Chain
	if err := json.Unmarshal(raw, &chain); err != nil {
		t.Fatal(err)
	}
	if err := chain.Validate(); err != nil {
		t.Fatalf("migrated chain invalid: %v\n%s", err, raw)
	}
	if len(chain.Phases) != 2 || chain.Phases[0].Blocks[0].Skill != "plan" || chain.Phases[1].Blocks[0].Skill != "build" {
		t.Fatalf("ordered plan/build work was not preserved: %+v", chain.Phases)
	}
	if chain.Phases[1].Blocks[1].Type != BlockCommand || chain.Phases[1].Limits.MaxRounds != 4 {
		t.Fatalf("gate or independent limits were not preserved: %+v", chain.Phases[1])
	}
}
