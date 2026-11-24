package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Runaway protection. A rule that starts this many runs within an hour, or
// whose trigger chain leads back to itself, is paused with a visible reason.
const (
	wakeupHourlyRunLimit     = 12
	wakeupDefaultMaxFires    = 20
	wakeupChainLimit         = 32
	wakeupLoopRepeats        = 2
	wakeupActivityCreated    = "wakeup_created"
	wakeupActivityTriggered  = "wakeup_triggered"
	wakeupActivityTimedOut   = "wakeup_timed_out"
	wakeupActivityCheckin    = "wakeup_checkin"
	wakeupActivityPaused     = "wakeup_paused"
	wakeupPausedLoop         = "loop"
	wakeupPausedRate         = "rate"
	wakeupPausedMaxFires     = "max_fires"
	wakeupTimeoutEventType   = "wakeup.timeout"
	wakeupManualEventType    = "wakeup.manual"
	wakeupConditionEventType = "condition.met"
)

// wakeupPreview is the rule snapshot stored with timeline entries, so the
// entry still reads correctly after the rule changes or is deleted.
func wakeupPreview(w db.IssueWakeup) map[string]any {
	preview := map[string]any{
		"id": util.UUIDToString(w.ID), "kind": w.Kind, "mode": w.Mode, "event_types": w.EventTypes,
		"agent_id": util.UUIDToString(w.AgentID), "timezone": w.Timezone,
	}
	if len(w.Condition) > 0 {
		preview["condition"] = json.RawMessage(w.Condition)
	}
	if w.FilterActorType.Valid {
		preview["filter_actor_type"] = w.FilterActorType.String
		preview["filter_actor_id"] = util.UUIDToString(w.FilterActorID)
	}
	if w.FilterAgentID.Valid {
		preview["filter_agent_id"] = util.UUIDToString(w.FilterAgentID)
	}
	if w.IntervalSeconds.Valid {
		preview["interval_seconds"] = w.IntervalSeconds.Int64
	}
	if w.CronExpression.Valid {
		preview["cron_expression"] = w.CronExpression.String
	}
	if w.NextFireAt.Valid && len(w.Condition) == 0 {
		preview["next_fire_at"] = w.NextFireAt.Time.UTC().Format(time.RFC3339)
	}
	if w.ExpiresAt.Valid {
		preview["expires_at"] = w.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}
	return preview
}

// wakeupActivity is written inside the rule's transaction and published after
// commit, so the timeline never shows an entry whose change rolled back.
type wakeupActivity struct {
	row       db.ActivityLog
	actorType string
	actorID   string
	details   json.RawMessage
}

func recordWakeupActivity(ctx context.Context, q *db.Queries, w db.IssueWakeup, action, actorType string, actorID pgtype.UUID, details map[string]any) (wakeupActivity, error) {
	if details == nil {
		details = map[string]any{}
	}
	preview := wakeupPreview(w)
	// Who the rule belongs to, for the entry's source: the agent whose run
	// created it, else the member.
	preview["created_by"] = util.UUIDToString(w.CreatedBy)
	if w.SourceTaskID.Valid {
		if source, err := q.GetAgentTask(ctx, w.SourceTaskID); err == nil {
			preview["created_by_agent_id"] = util.UUIDToString(source.AgentID)
		}
	}
	details["wakeup"] = preview
	raw, _ := json.Marshal(details)
	row, err := q.CreateActivity(ctx, db.CreateActivityParams{
		ID: dbid.NewV7(), WorkspaceID: w.WorkspaceID, IssueID: w.IssueID,
		ActorType: pgtype.Text{String: actorType, Valid: true}, ActorID: actorID,
		Action: action, Details: raw,
	})
	if err != nil {
		return wakeupActivity{}, err
	}
	id := ""
	if actorID.Valid {
		id = util.UUIDToString(actorID)
	}
	return wakeupActivity{row: row, actorType: actorType, actorID: id, details: raw}, nil
}

func (s *IssueWakeupService) publishWakeupActivities(activities ...wakeupActivity) {
	if s.Tasks == nil || s.Tasks.Bus == nil {
		return
	}
	for _, a := range activities {
		if !a.row.ID.Valid {
			continue
		}
		s.Tasks.Bus.Publish(events.Event{
			Type: protocol.EventActivityCreated, WorkspaceID: util.UUIDToString(a.row.WorkspaceID),
			ActorType: a.actorType, ActorID: a.actorID,
			Payload: map[string]any{
				"issue_id": util.UUIDToString(a.row.IssueID),
				"entry": map[string]any{
					"type": "activity", "id": util.UUIDToString(a.row.ID),
					"actor_type": a.actorType, "actor_id": a.actorID, "action": a.row.Action,
					"details": a.details, "created_at": a.row.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
				},
			},
		})
	}
}

// wakeupChain returns the causal chain for a new run of w: the rules that
// started the runs whose events triggered it, oldest first, ending with w.
// Of several source runs the one that passed through w most often wins. A
// chain that already passes through w wakeupLoopRepeats times means rules are
// waking each other without a person in between; a member's own action
// carries no source run and so starts a fresh chain. Allowing a repeat keeps a
// short review loop between two agents working.
func wakeupChain(ctx context.Context, q *db.Queries, w db.IssueWakeup, receipts []db.IssueWakeupReceipt) ([]string, bool, error) {
	var ids []pgtype.UUID
	seen := map[string]bool{}
	for _, r := range receipts {
		var refs struct {
			SourceTaskID string `json:"source_task_id"`
			TaskID       string `json:"task_id"`
		}
		_ = json.Unmarshal(r.Payload, &refs)
		for _, ref := range []string{refs.SourceTaskID, refs.TaskID} {
			if ref == "" || seen[ref] {
				continue
			}
			seen[ref] = true
			if id, err := util.ParseUUID(ref); err == nil {
				ids = append(ids, id)
			}
		}
	}
	self := util.UUIDToString(w.ID)
	count := func(chain []string) int {
		n := 0
		for _, id := range chain {
			if id == self {
				n++
			}
		}
		return n
	}
	var upstream []string
	if len(ids) > 0 {
		rows, err := q.ListWakeupChains(ctx, ids)
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			var chain []string
			_ = json.Unmarshal(row.Chain, &chain)
			if row.WakeupID != "" && (len(chain) == 0 || chain[len(chain)-1] != row.WakeupID) {
				chain = append(chain, row.WakeupID)
			}
			if upstream == nil || count(chain) > count(upstream) || (count(chain) == count(upstream) && len(chain) > len(upstream)) {
				upstream = chain
			}
		}
	}
	if count(upstream) >= wakeupLoopRepeats {
		return upstream, true, nil
	}
	chain := append(append([]string{}, upstream...), self)
	if len(chain) > wakeupChainLimit {
		chain = chain[len(chain)-wakeupChainLimit:]
	}
	return chain, false, nil
}

// wakeupTriggerDetails summarizes what woke the rule for its timeline entry:
// the event types, how many facts were merged, the condition observation and,
// for "wake now", who asked. References only, like the receipts themselves.
func wakeupTriggerDetails(task db.AgentTaskQueue, receipts []db.IssueWakeupReceipt) map[string]any {
	details := map[string]any{"task_id": util.UUIDToString(task.ID)}
	events := []string{}
	seen := map[string]bool{}
	count := int64(0)
	for _, r := range receipts {
		if !seen[r.EventType] {
			seen[r.EventType] = true
			events = append(events, r.EventType)
		}
		var payload struct {
			Count     int64           `json:"coalesced_count"`
			Observed  json.RawMessage `json:"observed"`
			ActorType string          `json:"actor_type"`
			ActorID   string          `json:"actor_id"`
		}
		_ = json.Unmarshal(r.Payload, &payload)
		count += max(payload.Count, 1)
		if len(payload.Observed) > 0 {
			details["observed"] = payload.Observed
		}
		if payload.ActorType == "member" || payload.ActorType == "agent" {
			details["actor_type"], details["actor_id"] = payload.ActorType, payload.ActorID
		}
	}
	details["events"] = events
	details["count"] = count
	return details
}
