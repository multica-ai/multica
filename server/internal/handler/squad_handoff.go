package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A squad in direct handoff mode lets members pass the issue to each other by
// @mention without waking the leader. Every pass is recorded as a
// squad_handoff activity; the round and daily guards below read those rows, so
// the guards only ever see handoffs this server actually routed.
//
// The limits and mode names live in the service package so the failure
// recovery path, which hands a failed member run to the leader, shares them.

const squadHandoffHistoryLimit = 50

// Handoff kinds stored in squad_handoff details. Only "member" counts toward
// the round and daily guards; "dispatch" is the leader handing out a step and
// resets the back-and-forth count.
const (
	squadHandoffKindDispatch = "dispatch"
	squadHandoffKindMember   = "member"
	squadHandoffKindHuman    = "human"
	squadHandoffKindLeader   = "leader"
)

// Reasons for a handoff that a guard sent back to the leader. A member reply
// that names no next owner goes back without one: it may be a finished issue
// as much as a stuck one.
const (
	squadHandoffReasonRoundLimit = "round_limit"
	squadHandoffReasonDailyLimit = "daily_limit"
)

// squadHandoffAuthor is an agent comment written by a run of a direct-mode
// squad's own leader or member on an issue assigned to that squad.
type squadHandoffAuthor struct {
	squad    db.Squad
	agentID  pgtype.UUID
	isLeader bool
}

// squadHandoffRecord is one handoff the comment will record once its routing
// has been enqueued. Records whose target agent did not end up with a run are
// dropped at write time.
type squadHandoffRecord struct {
	kind    string
	toType  string
	toID    pgtype.UUID
	round   int
	reason  string
	agentID pgtype.UUID // the agent whose run carries this handoff, if any
}

type squadHandoffPlan struct {
	author  *squadHandoffAuthor
	records []squadHandoffRecord
}

func (p *squadHandoffPlan) add(r squadHandoffRecord) {
	if p != nil {
		p.records = append(p.records, r)
	}
}

// directSquadHandoffAuthor resolves the squad context of an agent comment on a
// direct-mode squad issue. It fails closed to nil — today's routing — whenever
// the authoring run cannot be proven to be the squad's leader turn or a member
// turn on this very issue.
func (h *Handler) directSquadHandoffAuthor(ctx context.Context, issue db.Issue, actorType, actorID string, authoringTaskID pgtype.UUID) *squadHandoffAuthor {
	if actorType != "agent" || !authoringTaskID.Valid ||
		!issue.AssigneeType.Valid || issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid {
		return nil
	}
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || squad.ArchivedAt.Valid || squad.HandoffMode != service.SquadHandoffModeDirect {
		return nil
	}
	task, err := h.Queries.GetAgentTask(ctx, authoringTaskID)
	if err != nil || !task.AgentID.Valid || uuidToString(task.AgentID) != actorID || task.IssueID != issue.ID {
		return nil
	}
	if task.AgentID == squad.LeaderID {
		if !task.IsLeaderTask || task.SquadID != squad.ID {
			return nil
		}
		return &squadHandoffAuthor{squad: squad, agentID: task.AgentID, isLeader: true}
	}
	if task.IsLeaderTask || !h.isSquadAgentMember(ctx, squad, task.AgentID) {
		return nil
	}
	return &squadHandoffAuthor{squad: squad, agentID: task.AgentID}
}

func (h *Handler) isSquadAgentMember(ctx context.Context, squad db.Squad, agentID pgtype.UUID) bool {
	if agentID == squad.LeaderID {
		return false
	}
	ok, err := h.Queries.IsSquadMember(ctx, db.IsSquadMemberParams{SquadID: squad.ID, MemberType: "agent", MemberID: agentID})
	return err == nil && ok
}

// applyDirectSquadHandoff records the handoffs in an explicit-mention comment
// and enforces the member guards: a pass between two members that would start
// a round past the limit, or the issue's daily member-handoff budget running
// out, goes to the leader instead of the named member.
func (h *Handler) applyDirectSquadHandoff(ctx context.Context, issue db.Issue, author *squadHandoffAuthor, triggers []commentAgentTrigger, targets []commentMentionTarget, opts commentTriggerComputeOptions) ([]commentAgentTrigger, []commentMentionTarget) {
	plan := opts.handoffPlan
	if author.isLeader {
		for _, trigger := range triggers {
			if h.isSquadAgentMember(ctx, author.squad, trigger.Agent.ID) {
				plan.add(squadHandoffRecord{kind: squadHandoffKindDispatch, toType: "agent", toID: trigger.Agent.ID, agentID: trigger.Agent.ID})
			}
		}
		return triggers, targets
	}

	var history []db.ActivityLog
	historyLoaded := false
	var memberHandoffsToday int64 = -1
	redirect := ""
	kept := triggers[:0:0]
	for _, trigger := range triggers {
		if trigger.Source != commentTriggerSourceMentionAgent || trigger.Agent.ID == author.agentID ||
			!h.isSquadAgentMember(ctx, author.squad, trigger.Agent.ID) {
			kept = append(kept, trigger)
			continue
		}
		if !historyLoaded {
			history, _ = h.Queries.ListRecentSquadHandoffs(ctx, db.ListRecentSquadHandoffsParams{IssueID: issue.ID, RowLimit: squadHandoffHistoryLimit})
			historyLoaded = true
		}
		round := squadHandoffRound(history, author.agentID, trigger.Agent.ID)
		reason := ""
		if round > service.SquadHandoffRoundLimit {
			reason = squadHandoffReasonRoundLimit
		} else {
			if memberHandoffsToday < 0 {
				memberHandoffsToday, _ = h.Queries.CountSquadMemberHandoffsSince(ctx, db.CountSquadMemberHandoffsSinceParams{
					IssueID: issue.ID,
					Since:   pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true},
				})
			}
			if memberHandoffsToday >= service.SquadHandoffDailyLimit {
				reason = squadHandoffReasonDailyLimit
			}
		}
		if reason == "" {
			plan.add(squadHandoffRecord{kind: squadHandoffKindMember, toType: "agent", toID: trigger.Agent.ID, round: round, agentID: trigger.Agent.ID})
			kept = append(kept, trigger)
			memberHandoffsToday++
			continue
		}
		if redirect == "" {
			redirect = reason
		}
		agentID := uuidToString(trigger.Agent.ID)
		for i := range targets {
			if targets[i].ExecAgentID == agentID {
				targets[i].ExecAgentID = ""
				targets[i].Status = DispatchBlocked
				targets[i].ReasonCode = ReasonHandedToLeader
			}
		}
	}
	if redirect == "" {
		for _, trigger := range kept {
			if trigger.Source == commentTriggerSourceMentionAgent && trigger.Agent.ID == author.squad.LeaderID {
				plan.add(squadHandoffRecord{kind: squadHandoffKindLeader, toType: "agent", toID: author.squad.LeaderID, agentID: author.squad.LeaderID})
				break
			}
		}
		return kept, targets
	}
	for _, trigger := range kept {
		if trigger.Agent.ID == author.squad.LeaderID {
			plan.add(squadHandoffRecord{kind: squadHandoffKindLeader, toType: "agent", toID: author.squad.LeaderID, reason: redirect, agentID: author.squad.LeaderID})
			return kept, targets
		}
	}
	if leader, ok := h.routeAssignedSquadLeaderFallback(ctx, issue, "agent", uuidToString(author.agentID), opts); ok {
		leader.NonLeaderAgentReply = true
		kept = append(kept, leader)
		plan.add(squadHandoffRecord{kind: squadHandoffKindLeader, toType: "agent", toID: author.squad.LeaderID, reason: redirect, agentID: author.squad.LeaderID})
	}
	return kept, targets
}

// squadHandoffRound is the round a new from→to member handoff would start:
// one more than the from→to passes in the unbroken streak of member handoffs
// between the same two agents at the head of the history. Any other record —
// a leader dispatch, a third member, a pass to a human — ends the streak.
func squadHandoffRound(history []db.ActivityLog, from, to pgtype.UUID) int {
	round := 1
	for _, row := range history {
		var d struct {
			Kind string `json:"kind"`
			ToID string `json:"to_id"`
		}
		if json.Unmarshal(row.Details, &d) != nil || d.Kind != squadHandoffKindMember {
			break
		}
		rowFrom, rowTo := uuidToString(row.ActorID), d.ToID
		switch {
		case rowFrom == uuidToString(from) && rowTo == uuidToString(to):
			round++
		case rowFrom == uuidToString(to) && rowTo == uuidToString(from):
		default:
			return round
		}
	}
	return round
}

// recordSquadHandoffs writes the plan's squad_handoff rows after the comment's
// routing ran. A record whose agent did not get a run (refused, unchecked in
// the composer) is not a handoff. Re-routing the same comment — an edit — does
// not record it twice.
func (h *Handler) recordSquadHandoffs(ctx context.Context, issue db.Issue, comment db.Comment, plan *squadHandoffPlan, enqueued map[string]commentEnqueueResult) {
	if plan == nil || plan.author == nil || len(plan.records) == 0 {
		return
	}
	history, err := h.Queries.ListRecentSquadHandoffs(ctx, db.ListRecentSquadHandoffsParams{IssueID: issue.ID, RowLimit: squadHandoffHistoryLimit})
	if err != nil {
		slog.Warn("squad handoff: load history failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	commentID := uuidToString(comment.ID)
	for _, row := range history {
		var d struct {
			CommentID string `json:"comment_id"`
		}
		if json.Unmarshal(row.Details, &d) == nil && d.CommentID == commentID {
			return
		}
	}
	for _, rec := range plan.records {
		if rec.agentID.Valid {
			res, ok := enqueued[uuidToString(rec.agentID)]
			if !ok || res.status == DispatchBlocked {
				continue
			}
		}
		details := map[string]string{
			"squad_id":   uuidToString(plan.author.squad.ID),
			"comment_id": commentID,
			"kind":       rec.kind,
			"to_type":    rec.toType,
			"to_id":      uuidToString(rec.toID),
		}
		if rec.round > 1 {
			details["round"] = strconv.Itoa(rec.round)
		}
		if rec.reason != "" {
			details["reason"] = rec.reason
		}
		h.createSquadHandoffActivity(ctx, issue, plan.author.agentID, details)
	}
}

func (h *Handler) createSquadHandoffActivity(ctx context.Context, issue db.Issue, actorID pgtype.UUID, details map[string]string) {
	raw, _ := json.Marshal(details)
	activity, err := h.Queries.CreateActivity(ctx, db.CreateActivityParams{
		ID:          dbid.NewV7(),
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		ActorType:   pgtype.Text{String: "agent", Valid: true},
		ActorID:     actorID,
		Action:      service.SquadHandoffAction,
		Details:     raw,
	})
	if err != nil {
		slog.Warn("squad handoff: record failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	h.publish(protocol.EventActivityCreated, uuidToString(issue.WorkspaceID), "agent", uuidToString(actorID), map[string]any{
		"issue_id": uuidToString(issue.ID),
		"entry": map[string]any{
			"type":       "activity",
			"id":         uuidToString(activity.ID),
			"actor_type": "agent",
			"actor_id":   uuidToString(actorID),
			"action":     activity.Action,
			"details":    json.RawMessage(raw),
			"created_at": timestampToString(activity.CreatedAt),
		},
	})
}

// firstMemberMention is the human an agent comment hands the issue to.
func firstMemberMention(mentions []util.Mention) (pgtype.UUID, bool) {
	for _, m := range mentions {
		if m.Type != "member" {
			continue
		}
		if id, err := util.ParseUUID(m.ID); err == nil {
			return id, true
		}
	}
	return pgtype.UUID{}, false
}
