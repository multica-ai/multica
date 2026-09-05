package handler

import (
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A schedule lends only its immutable creator's identity, through either
// execution mode and through descendants that outlive the original run.
func TestIssueTaskTokensScheduleCreator(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, `[{"id":"erp","label":"ERP","env":"BOT_TOKEN_ERP","claims":{"sub":"{{identity.email}}"}}]`)
	for _, mode := range []string{"run_only", "create_issue"} {
		for _, scenario := range []string{"creator", "completed_root", "webhook", "webhook_kind", "wrong_creator", "agent_creator", "missing_creator", "offboarded_creator", "foreign_trigger", "missing_trigger"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				email := fmt.Sprintf("schedule-creator-%d@example.com", time.Now().UnixNano())
				creator := dbfx.User(t, "Schedule creator", email)
				if scenario != "offboarded_creator" {
					dbfx.Member(t, testWorkspaceID, creator, "member")
				}
				rt := handlerTestRuntimeID(t)
				agent := dbfx.Agent(t, "Schedule agent", rt, testutil.Cols{"owner_id": testUserID, "task_token_templates": `["erp"]`})
				apWorkspace := testWorkspaceID
				if scenario == "foreign_trigger" {
					apWorkspace = dbfx.Workspace(t, "Foreign schedule", fmt.Sprintf("foreign-schedule-%d", time.Now().UnixNano()))
				}
				ap := dbfx.Insert(t, "autopilot", testutil.Cols{"workspace_id": apWorkspace, "title": "Identity schedule", "assignee_id": agent, "execution_mode": mode, "created_by_type": "member", "created_by_id": testUserID})
				triggerCols := testutil.Cols{"autopilot_id": ap, "kind": "schedule", "created_by_type": "member", "created_by_id": creator, "published_by_type": "member", "published_by_id": testUserID, "cron_expression": "0 * * * *", "timezone": "UTC"}
				switch scenario {
				case "wrong_creator":
					triggerCols["created_by_id"] = testUserID
				case "agent_creator":
					triggerCols["created_by_type"], triggerCols["created_by_id"] = "agent", agent
				case "missing_creator":
					triggerCols["created_by_id"] = nil
				case "webhook_kind":
					triggerCols["kind"], triggerCols["cron_expression"], triggerCols["timezone"] = "webhook", nil, nil
				}
				trigger := dbfx.Insert(t, "autopilot_trigger", triggerCols)
				runCols := testutil.Cols{"autopilot_id": ap, "trigger_id": trigger, "source": "schedule", "status": "running"}
				if scenario == "webhook" {
					runCols["source"] = "webhook"
				}
				if scenario == "missing_trigger" {
					runCols["trigger_id"] = nil
				}
				if scenario == "completed_root" {
					runCols["status"] = "completed"
				}
				taskCols := testutil.Cols{"runtime_id": rt, "originator_user_id": creator, "accountable_user_id": creator, "originator_source": "trigger_owner"}
				if mode == "create_issue" {
					issue := dbfx.Issue(t, "Scheduled issue", testutil.Cols{"origin_type": "autopilot", "origin_id": ap})
					taskCols["issue_id"], runCols["issue_id"] = issue, issue
					dbfx.Cleanup(t, `DELETE FROM activity_log WHERE issue_id = $1`, issue)
				}
				run := dbfx.Insert(t, "autopilot_run", runCols)
				if mode == "run_only" {
					taskCols["autopilot_run_id"] = run
				}
				root := dbfx.Task(t, agent, taskCols)
				if mode == "run_only" {
					dbfx.Exec(t, `UPDATE autopilot_run SET task_id = $1 WHERE id = $2`, root, run)
				}
				wantToken := scenario == "creator" || scenario == "completed_root"
				assertSubject := func(taskID, agentID string) {
					t.Helper()
					task := loadTaskRow(t, taskID)
					got := testHandler.issueTaskTokens(t.Context(), &task, loadAgentRow(t, agentID), testWorkspaceID)
					token := got["BOT_TOKEN_ERP"]
					if !wantToken {
						if token != "" {
							t.Fatal("unproven schedule signed a human identity")
						}
						return
					}
					if token == "" {
						t.Fatal("valid schedule did not receive creator identity")
					}
					if sub := decodeJWTPayload(t, token)["sub"]; sub != email {
						t.Fatalf("signed %v, want creator %s", sub, email)
					}
				}
				assertSubject(root, agent)
				parent := root
				for _, source := range []string{"delegation", "comment_source"} {
					childAgent := dbfx.Agent(t, "Schedule descendant "+source, rt, testutil.Cols{"task_token_templates": `["erp"]`})
					child := dbfx.Task(t, childAgent, testutil.Cols{"runtime_id": rt, "originator_user_id": creator, "accountable_user_id": creator, "originator_source": source, "delegated_from_task_id": parent})
					assertSubject(child, childAgent)
					parent = child
				}
			})
		}
	}
}

func TestIssueTaskTokensRejectsIdentityChangeInLineage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	other := dbfx.User(t, "Other human", fmt.Sprintf("other-root-%d@example.com", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, other, "member")
	rt := handlerTestRuntimeID(t)
	rootAgent := dbfx.Agent(t, "Other human root", rt)
	root := dbfx.Task(t, rootAgent, testutil.Cols{"runtime_id": rt, "originator_source": "direct_human", "originator_user_id": other, "accountable_user_id": other})
	childAgent := dbfx.Agent(t, "Changed human hop", rt)
	child := dbfx.Task(t, childAgent, testutil.Cols{"runtime_id": rt, "originator_source": "delegation", "originator_user_id": testUserID, "accountable_user_id": testUserID, "delegated_from_task_id": root})
	leafAgent := dbfx.Agent(t, "Changed human leaf", rt, testutil.Cols{"task_token_templates": `["erp"]`})
	leaf := dbfx.Task(t, leafAgent, testutil.Cols{"runtime_id": rt, "originator_source": "comment_source", "originator_user_id": other, "accountable_user_id": other, "delegated_from_task_id": child})
	task := loadTaskRow(t, leaf)
	if got := testHandler.issueTaskTokens(t.Context(), &task, loadAgentRow(t, leafAgent), testWorkspaceID); len(got) != 0 {
		t.Fatal("identity substitution in an intermediate hop was accepted")
	}
}
