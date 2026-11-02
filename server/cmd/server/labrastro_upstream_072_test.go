package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestExternalConversationCannotSteerMemberTurn(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprintf("named=%t", named), func(t *testing.T) {
			f := newExternalPolicyFixture(t, "root")
			f.fx.Exec(t, "UPDATE agent_runtime SET provider='claude' WHERE id=$1", f.runtime)
			target := f.fx.Agent(t, "Steer target", f.runtime, testutil.Cols{"owner_id": testUserID})
			issue := policyWakeIssue(t, f, target)
			running := f.fx.Task(t, target, testutil.Cols{"runtime_id": f.runtime, "issue_id": issue, "status": "running", "originator_user_id": testUserID, "accountable_user_id": testUserID, "originator_source": "direct_human"})
			f.fx.Cleanup(t, "DELETE FROM task_supplement WHERE task_id=$1", running)
			f.fx.InsertNoID(t, "task_supplement_capability", testutil.Cols{"task_id": running, "workspace_id": testWorkspaceID, "issue_id": issue, "capability": protocol.DaemonCapabilityTaskSupplementV1}, "task_id=$1", running)
			body := map[string]any{"content": fmt.Sprintf("[@Target](mention://agent/%s) follow up", target), "steer": !named}
			if named {
				body["steer_task_ids"] = []string{running}
			}
			w := policyRequest(t, f.token, "POST", "/api/issues/"+issue+"/comments", body)
			if w.Code != http.StatusCreated {
				t.Fatalf("comment: %d %s", w.Code, w.Body.String())
			}
			if n := f.fx.Count(t, "SELECT count(*) FROM task_supplement WHERE task_id=$1", running); n != 0 {
				t.Fatalf("external comment injected %d supplements into a member turn", n)
			}
			var externalComment struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &externalComment); err != nil {
				t.Fatal(err)
			}
			var root, grantor, source, evidence, evidenceRef string
			f.fx.QueryRow(t, "SELECT conversation_root_task_id::text, originator_user_id::text, originator_source, trigger_evidence_kind, trigger_evidence_ref_id::text FROM agent_task_queue WHERE issue_id=$1 AND id<>$2", issue, running).Scan(&root, &grantor, &source, &evidence, &evidenceRef)
			if source != "delegation" || evidence != "comment" || evidenceRef != externalComment.ID {
				t.Fatalf("follow-up lost comment evidence: source=%s kind=%s ref=%s want=%s", source, evidence, evidenceRef, externalComment.ID)
			}
			if root != f.root || grantor != testUserID {
				t.Fatalf("follow-up lost grant: root=%s grantor=%s", root, grantor)
			}
			w = policyRequest(t, f.token, "GET", "/api/issues/"+issue+"/task-runs", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("list runs: %d %s", w.Code, w.Body.String())
			}
			var runs []handler.AgentTaskResponse
			if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
				t.Fatal(err)
			}
			for _, run := range runs {
				if run.CanSupplement {
					t.Fatal("external caller was offered can_supplement")
				}
			}
			// The same running turn must accept the same request from a member.
			w = policyRequest(t, testToken, "POST", "/api/issues/"+issue+"/comments", body)
			if w.Code != http.StatusCreated {
				t.Fatalf("member comment: %d %s", w.Code, w.Body.String())
			}
			var memberComment struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &memberComment); err != nil {
				t.Fatal(err)
			}
			if n := f.fx.Count(t, "SELECT count(*) FROM task_supplement WHERE task_id=$1 AND comment_id=$2", running, memberComment.ID); n != 1 {
				t.Fatalf("member positive control bound %d supplements, want 1", n)
			}
		})
	}
}

func TestExternalConversationMemberRerunKeepsFrozenGrant(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		for _, withComment := range []bool{false, true} {
			t.Run(fmt.Sprintf("fresh=%t/comment=%t", fresh, withComment), func(t *testing.T) {
				fx := testutil.New(testPool, testWorkspaceID, testUserID)
				grantor := fx.User(t, "Rerun grantor", fmt.Sprintf("rerun-%t-%t@example.test", fresh, withComment))
				fx.Member(t, testWorkspaceID, grantor, "member")
				f := externalPolicyFixtureForGrantor(t, "root", grantor)
				fx.Exec(t, "UPDATE agent SET visibility='workspace', permission_mode='public_to' WHERE id=$1", f.agent)
				fx.Exec(t, "INSERT INTO agent_invocation_target (agent_id,target_type,target_id) VALUES ($1,'workspace',$2)", f.agent, testWorkspaceID)
				issue := policyWakeIssue(t, f, f.agent)
				cols := testutil.Cols{"runtime_id": f.runtime, "issue_id": issue, "status": "completed", "originator_user_id": grantor, "accountable_user_id": grantor, "originator_source": "delegation", "delegated_from_task_id": f.root}
				if withComment {
					cols["trigger_comment_id"] = fx.Comment(t, issue, "Member comment retained as prompt context")
				}
				source := fx.Task(t, f.agent, cols)
				w := policyRequest(t, testToken, "POST", "/api/tasks/"+source+"/rerun", map[string]any{"fresh": fresh})
				if w.Code != http.StatusAccepted {
					t.Fatalf("member rerun: %d %s", w.Code, w.Body.String())
				}
				var pendingID, pendingStatus, root, human, accountable string
				var gotFresh bool
				fx.QueryRow(t, "SELECT id::text, status, COALESCE(conversation_root_task_id::text,''), originator_user_id::text, accountable_user_id::text, COALESCE((context->>'fresh_rerun')::bool,false) FROM agent_task_queue WHERE rerun_of_task_id=$1", source).Scan(&pendingID, &pendingStatus, &root, &human, &accountable, &gotFresh)
				if root != f.root || human != grantor || accountable != grantor || gotFresh != fresh {
					t.Fatalf("rerun elevated authority: root=%s human=%s accountable=%s fresh=%t", root, human, accountable, gotFresh)
				}
				if pendingStatus != "queued" {
					t.Fatalf("rerun status=%s, want queued", pendingStatus)
				}
				fx.Exec(t, "UPDATE channel_installation SET status='revoked' WHERE id=$1", f.install)
				svc := service.NewTaskService(db.New(testPool), testPool, realtime.NewHub(), events.New())
				before := fx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue)
				_, err := svc.RerunIssue(context.Background(), policyUUID(t, issue), policyUUID(t, source), pgtype.UUID{}, policyUUID(t, testUserID), fresh, nil)
				if err != channel.ErrConversationDenied {
					t.Fatalf("revoked source: %v", err)
				}
				if got := fx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue); got != before {
					t.Fatal("revoked rerun changed the task count")
				}
				var afterStatus string
				fx.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id=$1", pendingID).Scan(&afterStatus)
				if afterStatus != pendingStatus {
					t.Fatalf("revoked rerun mutated pending task %s: %s -> %s", pendingID, pendingStatus, afterStatus)
				}
			})
		}
	}
}

func TestExternalConversationTaskRerunRouteDenied(t *testing.T) {
	f := newExternalPolicyFixture(t, "root")
	w := policyRequest(t, f.token, "POST", "/api/tasks/"+f.root+"/rerun", map[string]bool{"fresh": true})
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "external_conversation_forbidden") {
		t.Fatalf("new write route escaped policy: %d %s", w.Code, w.Body.String())
	}
}
