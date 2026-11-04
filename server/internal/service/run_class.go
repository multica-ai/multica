package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RunClass is what started a run, as the company controls and the budget gate
// see it (spec 4.2 and 4.5).
type RunClass string

const (
	// RunClassHuman: a person started the run by commenting, chatting, assigning,
	// running an autopilot by hand or rerunning a task. Never held.
	RunClassHuman RunClass = "human"
	// RunClassRoutine: an autopilot schedule or webhook, a wakeup, or an
	// operating review started it. Held when paused and deferred in quiet hours.
	RunClassRoutine RunClass = "routine"
	// RunClassDelegated: an agent started it for another agent, through a
	// mention, a sub-issue or a reply to an agent's comment.
	RunClassDelegated RunClass = "delegated"
	// RunClassExempt: the Secretary's daily brief and weekly review, and catch-up
	// runs. They keep working through pause, quiet hours and budget holds.
	RunClassExempt RunClass = "exempt"
)

// RunClassInput is what run class resolution reads. The task row supplies
// OriginatorSource and HasChatSession. An autopilot run that enqueued the task
// supplies AutopilotSource and SecretaryBrief.
type RunClassInput struct {
	// OriginatorSource is agent_task_queue.originator_source, "" when NULL.
	OriginatorSource string
	// HasChatSession is true when the task belongs to a chat session a person
	// is typing into.
	HasChatSession bool
	// AutopilotSource is autopilot_run.source of the enqueuing run, "" when the
	// task did not come from an autopilot run.
	AutopilotSource string
	// SecretaryBrief is true when that autopilot is the workspace's Secretary's
	// daily brief or weekly review.
	SecretaryBrief bool
}

// ResolveRunClass decides a run's class. The rules, in order:
//
//  1. A catch-up run and the Secretary's brief are exempt.
//  2. An autopilot run started by hand is human, as is a chat turn.
//  3. A task whose originator is a person (originator_source direct_human,
//     which covers comments, assignments, manual triggers and reruns) is human.
//  4. A task that an agent started for another agent (originator_source
//     delegation or comment_source) is delegated.
//  5. Everything else is routine: autopilot schedules and webhooks (whose
//     originator_source is trigger_owner), wakeups, and runs with no human
//     resolved (owner_fallback, unattributed, backfill, rule_owner).
//
// Retries keep their parent's originator_source (spec attribution), so a retry
// keeps its parent's class.
func ResolveRunClass(in RunClassInput) RunClass {
	switch {
	case in.AutopilotSource == BriefCatchupSource || in.SecretaryBrief:
		return RunClassExempt
	case in.AutopilotSource == "manual" || in.HasChatSession:
		return RunClassHuman
	case in.OriginatorSource == "direct_human":
		return RunClassHuman
	case in.OriginatorSource == "delegation" || in.OriginatorSource == "comment_source":
		return RunClassDelegated
	default:
		return RunClassRoutine
	}
}

// RunClassOfTask resolves the class of a queued or running task. Only a task
// that an autopilot run enqueued costs a second read, so the claim path pays
// for one indexed lookup at most.
func RunClassOfTask(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (RunClass, error) {
	in := RunClassInput{
		OriginatorSource: task.OriginatorSource.String,
		HasChatSession:   task.ChatSessionID.Valid,
	}
	if task.AutopilotRunID.Valid {
		row, err := q.GetAutopilotRunClassInputs(ctx, task.AutopilotRunID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// The run row is gone; classify by the task alone.
		case err != nil:
			return "", fmt.Errorf("load autopilot run for run class: %w", err)
		default:
			in.AutopilotSource = row.Source
			in.SecretaryBrief = isSecretaryBriefAutopilot(row.Title, row.AssigneeType, row.AssigneeID, row.SecretaryAgentID)
		}
	}
	return ResolveRunClass(in), nil
}

// isSecretaryBriefAutopilot reports whether an autopilot is the Secretary's
// daily brief or weekly review: the titles the office setup gives them, assigned
// to the workspace's current Secretary.
func isSecretaryBriefAutopilot(title, assigneeType string, assigneeID, secretaryID pgtype.UUID) bool {
	if assigneeType != "agent" || !secretaryID.Valid || assigneeID != secretaryID {
		return false
	}
	return title == DailyBriefTitle || title == WeeklyReviewTitle
}
