package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestResolveRunClassTable pins the class rules of spec 4.2 and 4.5. The
// attribution sources are the ones the task row records.
func TestResolveRunClassTable(t *testing.T) {
	cases := []struct {
		name string
		in   RunClassInput
		want RunClass
	}{
		{"a person's comment or assignment", RunClassInput{OriginatorSource: "direct_human"}, RunClassHuman},
		{"a chat turn", RunClassInput{HasChatSession: true, OriginatorSource: "direct_human"}, RunClassHuman},
		{"a chat turn with no originator yet", RunClassInput{HasChatSession: true}, RunClassHuman},
		{"a manual autopilot run", RunClassInput{AutopilotSource: "manual", OriginatorSource: "trigger_owner"}, RunClassHuman},
		{"an autopilot schedule", RunClassInput{AutopilotSource: "schedule", OriginatorSource: "trigger_owner"}, RunClassRoutine},
		{"an autopilot webhook", RunClassInput{AutopilotSource: "webhook", OriginatorSource: "trigger_owner"}, RunClassRoutine},
		{"a wakeup", RunClassInput{OriginatorSource: "trigger_owner"}, RunClassRoutine},
		{"a run with no human resolved", RunClassInput{OriginatorSource: "owner_fallback"}, RunClassRoutine},
		{"a rule run with no principal", RunClassInput{OriginatorSource: "rule_owner"}, RunClassRoutine},
		{"a run with no attribution at all", RunClassInput{}, RunClassRoutine},
		{"an agent mentioning another agent", RunClassInput{OriginatorSource: "delegation"}, RunClassDelegated},
		{"an agent reacting to an agent's comment", RunClassInput{OriginatorSource: "comment_source"}, RunClassDelegated},
		{"the Secretary's daily brief", RunClassInput{AutopilotSource: "schedule", OriginatorSource: "trigger_owner", SecretaryBrief: true}, RunClassExempt},
		{"the Secretary's brief started by hand", RunClassInput{AutopilotSource: "manual", SecretaryBrief: true}, RunClassExempt},
		{"a catch-up run", RunClassInput{AutopilotSource: BriefCatchupSource, OriginatorSource: "trigger_owner"}, RunClassExempt},
		{"a catch-up run of another autopilot", RunClassInput{AutopilotSource: BriefCatchupSource}, RunClassExempt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveRunClass(tc.in); got != tc.want {
				t.Fatalf("ResolveRunClass(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSecretaryBriefAutopilotNeedsTheSecretaryAsAssignee(t *testing.T) {
	secretary := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	other := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	cases := []struct {
		title, assigneeType string
		assignee            pgtype.UUID
		want                bool
	}{
		{DailyBriefTitle, "agent", secretary, true},
		{WeeklyReviewTitle, "agent", secretary, true},
		{DailyBriefTitle, "agent", other, false},
		{DailyBriefTitle, "squad", secretary, false},
		{"Morning digest", "agent", secretary, false},
	}
	for _, tc := range cases {
		if got := isSecretaryBriefAutopilot(tc.title, tc.assigneeType, tc.assignee, secretary); got != tc.want {
			t.Errorf("isSecretaryBriefAutopilot(%q, %q) = %v, want %v", tc.title, tc.assigneeType, got, tc.want)
		}
	}
	if isSecretaryBriefAutopilot(DailyBriefTitle, "agent", secretary, pgtype.UUID{}) {
		t.Error("a workspace with no Secretary has no Secretary brief")
	}
}

// TestRunClassOfTaskReadsTheEnqueuingAutopilotRun checks the one lookup the
// claim path makes: a task that an autopilot run enqueued resolves through that
// run's source and, for the Secretary's brief, its title and assignee.
func TestRunClassOfTaskReadsTheEnqueuingAutopilotRun(t *testing.T) {
	f, owner := newPrincipalFixture(t)
	ctx := context.Background()
	agent := f.privateAgentOwnedBy(t, owner, "class")
	runtime := f.Runtime(t, "class-runtime", testutil.Cols{"owner_id": owner})
	f.Exec(t, `UPDATE workspace SET secretary_agent_id = $1 WHERE id = $2`, agent, f.WorkspaceID)

	briefAP := f.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": f.WorkspaceID, "title": DailyBriefTitle, "assignee_type": "agent",
		"assignee_id": agent, "status": "active", "execution_mode": "run_only",
		"created_by_type": "member", "created_by_id": owner,
	})
	otherAP := f.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": f.WorkspaceID, "title": "Triage inbox", "assignee_type": "agent",
		"assignee_id": agent, "status": "active", "execution_mode": "run_only",
		"created_by_type": "member", "created_by_id": owner,
	})

	cases := []struct {
		name, autopilot, source string
		want                    RunClass
	}{
		{"the brief on schedule", briefAP, "schedule", RunClassExempt},
		{"the brief caught up", briefAP, BriefCatchupSource, RunClassExempt},
		{"another autopilot on schedule", otherAP, "schedule", RunClassRoutine},
		{"another autopilot by hand", otherAP, "manual", RunClassHuman},
	}
	for _, tc := range cases {
		run := f.Insert(t, "autopilot_run", testutil.Cols{"autopilot_id": tc.autopilot, "source": tc.source, "status": "running"})
		task := f.Task(t, agent, testutil.Cols{"runtime_id": runtime, "autopilot_run_id": run, "originator_source": "trigger_owner"})
		got, err := RunClassOfTask(ctx, f.q, mustLoadTask(t, f, task))
		if err != nil {
			t.Fatalf("%s: RunClassOfTask: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: class = %q, want %q", tc.name, got, tc.want)
		}
	}

	// A task with no autopilot run is classified by the task alone.
	plain := f.Task(t, agent, testutil.Cols{"runtime_id": runtime, "originator_source": "direct_human"})
	if got, err := RunClassOfTask(ctx, f.q, mustLoadTask(t, f, plain)); err != nil || got != RunClassHuman {
		t.Fatalf("a comment task = %q, %v; want human", got, err)
	}
}

func mustLoadTask(t *testing.T, f principalFixture, id string) db.AgentTaskQueue {
	t.Helper()
	task, err := f.q.GetAgentTask(context.Background(), util.MustParseUUID(id))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	return task
}
