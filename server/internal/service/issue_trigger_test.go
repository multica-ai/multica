package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// An issue write that could have started a run but did not names why, so the
// write paths can log it (#10845). Writes that never start a run, such as a
// title edit, carry no reason and therefore log nothing.
func TestWillEnqueueRunNamesWhyAWriteStartsNoRun(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	q := db.New(pool)
	bootstrap := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()
	userID := bootstrap.User(t, "trigger-owner", fmt.Sprintf("trigger-owner-%d@example.com", suffix))
	workspaceID := bootstrap.Workspace(t, "issue trigger reasons", fmt.Sprintf("issue-trigger-%d", suffix))
	f := testutil.New(pool, workspaceID, userID)
	f.Member(t, workspaceID, userID, "owner")
	if err := q.SeedIssueStatusEntries(ctx, util.MustParseUUID(workspaceID)); err != nil {
		t.Fatalf("seed statuses: %v", err)
	}
	runtimeID := f.Runtime(t, "online runtime")
	agentID := f.Agent(t, "ready agent", runtimeID)
	unboundAgentID := f.Agent(t, "unbound agent", "")
	archivedAgentID := f.Agent(t, "archived agent", runtimeID, testutil.Cols{"archived_at": testutil.Raw("now()")})
	squadID := f.Squad(t, "squad with unbound leader", f.Agent(t, "unbound leader", ""))
	svc := &IssueService{Queries: q, TaskService: NewTaskService(q, pool, nil, events.New())}

	assigned := func(assigneeType, assigneeID, status string) testutil.Cols {
		return testutil.Cols{"assignee_type": assigneeType, "assignee_id": assigneeID, "status": status}
	}
	assign := func(issue db.Issue) IssueTriggerInput {
		return IssueTriggerInput{Issue: issue, PrevStatus: issue.Status, AssigneeChanged: true}
	}
	create := func(issue db.Issue) IssueTriggerInput {
		return IssueTriggerInput{Issue: issue, IsCreate: true}
	}
	promote := func(issue db.Issue) IssueTriggerInput {
		return IssueTriggerInput{Issue: issue, PrevStatus: "backlog", StatusChanged: true}
	}
	edit := func(issue db.Issue) IssueTriggerInput {
		return IssueTriggerInput{Issue: issue, PrevStatus: issue.Status}
	}
	yes := func(pgtype.UUID) bool { return true }

	for _, tc := range []struct {
		name        string
		issue       testutil.Cols
		input       func(db.Issue) IssueTriggerInput
		probe       IssueTriggerProbe
		pendingTask bool
		wantRun     bool
		want        dispatch.ReasonCode
	}{
		{name: "assignment starts a run", issue: assigned("agent", agentID, "todo"), input: assign, wantRun: true},
		{name: "edit that triggers nothing", issue: assigned("agent", agentID, "todo"), input: edit},
		{name: "unassigned issue", issue: testutil.Cols{"status": "todo"}, input: promote},
		{name: "promotion straight to done", issue: assigned("agent", agentID, "done"), input: promote},
		{name: "edit while in triage", issue: inTriage(assigned("agent", agentID, "todo")), input: edit},
		{name: "assignment while in triage", issue: inTriage(assigned("agent", agentID, "todo")), input: assign, want: dispatch.ReasonIssueInTriage},
		{name: "assignment into backlog", issue: assigned("agent", agentID, "backlog"), input: assign, want: dispatch.ReasonDeferred},
		{name: "member assignment while in triage", issue: inTriage(assigned("member", userID, "todo")), input: assign},
		{name: "member assignment into backlog", issue: assigned("member", userID, "backlog"), input: assign},
		{name: "member creation while in triage", issue: inTriage(assigned("member", userID, "todo")), input: create},
		{name: "member creation in backlog", issue: assigned("member", userID, "backlog"), input: create},
		{name: "member promotion while in triage", issue: inTriage(assigned("member", userID, "todo")), input: promote},
		{name: "member promotion out of backlog", issue: assigned("member", userID, "todo"), input: promote},
		{name: "squad assignment while in triage", issue: inTriage(assigned("squad", squadID, "todo")), input: assign, want: dispatch.ReasonIssueInTriage},
		{name: "squad assignment into backlog", issue: assigned("squad", squadID, "backlog"), input: assign, want: dispatch.ReasonDeferred},
		{name: "agent without a runtime", issue: assigned("agent", unboundAgentID, "todo"), input: assign, want: dispatch.ReasonAgentRuntimeRequired},
		{name: "archived agent", issue: assigned("agent", archivedAgentID, "todo"), input: assign, want: dispatch.ReasonTargetUnavailable},
		{name: "agent the caller cannot invoke", issue: assigned("agent", agentID, "todo"), input: assign,
			probe: IssueTriggerProbe{CanAccessAgent: func(db.Agent) bool { return false }}, want: dispatch.ReasonInvocationNotAllowed},
		{name: "promotion from the assignee's own run", issue: assigned("agent", agentID, "todo"), input: promote,
			probe: IssueTriggerProbe{IsSelfLoop: yes}, want: dispatch.ReasonAlreadyActive},
		{name: "agent assigning itself while already active", issue: assigned("agent", agentID, "todo"), input: assign,
			probe: IssueTriggerProbe{SuppressActiveSelfAssignment: yes}, want: dispatch.ReasonAlreadyActive},
		{name: "promotion with a run already pending", issue: assigned("agent", agentID, "todo"), input: promote,
			pendingTask: true, want: dispatch.ReasonCoalesced},
		{name: "squad whose leader has no runtime", issue: assigned("squad", squadID, "todo"), input: assign, want: dispatch.ReasonAgentRuntimeRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issueID := f.Issue(t, tc.name, tc.issue)
			f.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
			if tc.pendingTask {
				f.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "queued"})
			}
			issue, err := q.GetIssue(ctx, util.MustParseUUID(issueID))
			if err != nil {
				t.Fatalf("load issue: %v", err)
			}

			trigger, willRun := svc.WillEnqueueRun(ctx, tc.input(issue), tc.probe)
			if willRun != tc.wantRun || trigger.DeclineReason != tc.want {
				t.Fatalf("WillEnqueueRun = run:%v reason:%q, want run:%v reason:%q", willRun, trigger.DeclineReason, tc.wantRun, tc.want)
			}
		})
	}
}

func inTriage(cols testutil.Cols) testutil.Cols {
	cols["triage_state"] = "pending"
	return cols
}
