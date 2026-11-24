package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Squad handoff modes. "leader" is the original behaviour: every member reply
// goes back to the squad leader, who dispatches the next step. "direct" lets
// members hand the issue to each other by @mention; the leader is woken only
// for exceptions.
const (
	SquadHandoffModeLeader = "leader"
	SquadHandoffModeDirect = "direct"
)

// SquadHandoffAction is the activity_log action of one recorded squad handoff.
const SquadHandoffAction = "squad_handoff"

// Guards for direct handoff. Fixed on purpose: they are backstops against
// members passing work around forever, not tuning knobs.
const (
	// SquadHandoffRoundLimit is how many times one member may hand the issue
	// to the same other member in an unbroken back-and-forth.
	SquadHandoffRoundLimit = 3
	// SquadHandoffDailyLimit is how many member-to-member handoffs one issue
	// may take in 24 hours.
	SquadHandoffDailyLimit = 20
)

// IsValidSquadHandoffMode reports whether mode is a handoff mode the API accepts.
func IsValidSquadHandoffMode(mode string) bool {
	return mode == SquadHandoffModeLeader || mode == SquadHandoffModeDirect
}

// squadDirectHandoffLeader returns the squad whose leader should recover a
// failed member run instead of the member that handed the work over. That is
// the case only for a direct-handoff squad's issue, when the run that handed
// off was a member turn on that same issue: a leader dispatch already recovers
// to the leader, and a handoff across issues keeps its own coordinator.
func squadDirectHandoffLeader(ctx context.Context, q *db.Queries, issue db.Issue, failed, source db.AgentTaskQueue) *db.Squad {
	if source.IsLeaderTask || failed.IssueID != issue.ID ||
		!issue.AssigneeType.Valid || issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid {
		return nil
	}
	squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
	if err != nil || squad.ArchivedAt.Valid || squad.HandoffMode != SquadHandoffModeDirect || squad.LeaderID == source.AgentID {
		return nil
	}
	isMember, err := q.IsSquadMember(ctx, db.IsSquadMemberParams{SquadID: squad.ID, MemberType: "agent", MemberID: source.AgentID})
	if err != nil || !isMember {
		return nil
	}
	return &squad
}

// createSquadFailureHandoff records the failed member handing the issue back
// to its squad leader, in the transaction that creates the recovery signal.
func createSquadFailureHandoff(ctx context.Context, qtx *db.Queries, target *delegatedFailureRecoveryTarget) (db.ActivityLog, error) {
	details, _ := json.Marshal(map[string]string{
		"squad_id":   util.UUIDToString(target.leaderSquad.ID),
		"comment_id": util.UUIDToString(target.comment.ID),
		"kind":       "leader",
		"to_type":    "agent",
		"to_id":      util.UUIDToString(target.leaderSquad.LeaderID),
		"reason":     "run_failed",
	})
	row, err := qtx.CreateActivity(ctx, db.CreateActivityParams{
		ID:          dbid.NewV7(),
		WorkspaceID: target.issue.WorkspaceID,
		IssueID:     target.issue.ID,
		ActorType:   pgtype.Text{String: "agent", Valid: true},
		ActorID:     target.failed.AgentID,
		Action:      SquadHandoffAction,
		Details:     details,
	})
	if err != nil {
		return db.ActivityLog{}, fmt.Errorf("record squad failure handoff: %w", err)
	}
	return row, nil
}

func (s *TaskService) publishSquadHandoffActivity(row db.ActivityLog) {
	if s.Bus == nil {
		return
	}
	actorID := util.UUIDToString(row.ActorID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventActivityCreated,
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		ActorType:   "agent",
		ActorID:     actorID,
		Payload: map[string]any{
			"issue_id": util.UUIDToString(row.IssueID),
			"entry": map[string]any{
				"type":       "activity",
				"id":         util.UUIDToString(row.ID),
				"actor_type": "agent",
				"actor_id":   actorID,
				"action":     row.Action,
				"details":    json.RawMessage(row.Details),
				"created_at": row.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
			},
		},
	})
}
