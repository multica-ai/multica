package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Runs are serialized per issue and agent, so a wakeup never runs alongside
// another run of its agent; it runs after it. Two cases would make that later
// run a repeat, and a firing that hits either one starts no run of its own:
//
//   - acknowledged: every input came from the agent itself, so it already
//     knows. An issue, comment, reaction or attachment event the agent
//     produced never wakes it. A satisfied condition its own unfinished run on
//     the issue brought about does not wake it either, when the rule is the
//     platform's or the agent set it up itself; a person's condition rule
//     carries an instruction the running agent does not have, so it still
//     runs afterwards.
//   - merged: a run of the agent is already waiting to start on the issue.
//     The rule's instruction and facts join that run (context.wakeup_joined,
//     rendered into its prompt) instead of queuing another.
const (
	wakeupOutcomeMerged       = "merged"
	wakeupOutcomeAcknowledged = "acknowledged"
	maxJoinedWakeupNote       = 12000
)

// receiptActor reads who produced an event receipt. Coalesced receipts keep
// only the latest actor, so they never count as the agent's own.
func receiptActor(r db.IssueWakeupReceipt) (actorType, actorID string, single bool) {
	var p struct {
		ActorType string `json:"actor_type"`
		ActorID   string `json:"actor_id"`
		AgentID   string `json:"agent_id"`
		Count     int64  `json:"coalesced_count"`
	}
	_ = json.Unmarshal(r.Payload, &p)
	if p.ActorType == "" && p.AgentID != "" {
		p.ActorType, p.ActorID = "agent", p.AgentID
	}
	return p.ActorType, p.ActorID, p.Count <= 1
}

// acknowledgedBySelf reports whether every input of this firing came from the
// target agent itself (see the rules above).
func acknowledgedBySelf(ctx context.Context, q *db.Queries, w db.IssueWakeup, agentID, issueID pgtype.UUID, receipts []db.IssueWakeupReceipt) (bool, error) {
	if len(receipts) == 0 {
		return false, nil
	}
	agent := util.UUIDToString(agentID)
	ownRule := w.SystemRule.Valid
	if !ownRule && w.SourceTaskID.Valid {
		source, err := q.GetAgentTask(ctx, w.SourceTaskID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
		ownRule = err == nil && source.AgentID == agentID
	}
	seen := map[pgtype.UUID]bool{}
	var causes []pgtype.UUID
	for _, r := range receipts {
		switch {
		case r.EventType == wakeupConditionEventType:
			if !ownRule {
				return false, nil
			}
			var p struct {
				SourceTaskIDs []string `json:"source_task_ids"`
			}
			_ = json.Unmarshal(r.Payload, &p)
			if len(p.SourceTaskIDs) == 0 {
				return false, nil
			}
			for _, raw := range p.SourceTaskIDs {
				id, err := util.ParseUUID(raw)
				if err != nil {
					return false, nil
				}
				if !seen[id] {
					seen[id] = true
					causes = append(causes, id)
				}
			}
		case strings.HasPrefix(r.EventType, "task."), r.EventType == "time.due", r.EventType == wakeupManualEventType, r.EventType == wakeupTimeoutEventType:
			// A run ending, a time, a person's "wake now" and a deadline are
			// never the agent's own action.
			return false, nil
		default:
			// A rule that names the agent itself as the actor asked for this.
			if w.FilterActorType.String == "agent" && w.FilterActorID == agentID {
				return false, nil
			}
			actorType, actorID, single := receiptActor(r)
			if !single || actorType != "agent" || actorID != agent {
				return false, nil
			}
		}
	}
	if len(causes) == 0 {
		return true, nil
	}
	active, err := q.CountActiveIssueRunsOfAgent(ctx, db.CountActiveIssueRunsOfAgentParams{Ids: causes, AgentID: agentID, IssueID: issueID})
	if err != nil {
		return false, err
	}
	return int(active) == len(causes), nil
}

// joinedWakeupNote is what the waiting run is told about a rule that joined it.
func joinedWakeupNote(w db.IssueWakeup, instruction string, receipts []db.IssueWakeupReceipt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Wakeup %s fired while this run was waiting to start, so it did not start a run of its own; handle it in this run. Instruction:\n%s\nTrigger facts (read current state before deciding what to do):\n", util.UUIDToString(w.ID), instruction)
	for _, r := range receipts {
		line := r.EventType + " " + string(canonicalWakeupPayload(r.Payload)) + "\n"
		if b.Len()+len(line) > maxJoinedWakeupNote {
			b.WriteString(wakeupOmittedEvidence)
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

// avoidRedundantRun returns acknowledged or merged when the firing needs no
// run of its own, with the run it joined; empty means start a run.
func avoidRedundantRun(ctx context.Context, q *db.Queries, w db.IssueWakeup, instruction string, agentID, issueID pgtype.UUID, receipts []db.IssueWakeupReceipt) (string, db.AgentTaskQueue, error) {
	self, err := acknowledgedBySelf(ctx, q, w, agentID, issueID, receipts)
	if err != nil || self {
		if self {
			return wakeupOutcomeAcknowledged, db.AgentTaskQueue{}, nil
		}
		return "", db.AgentTaskQueue{}, err
	}
	entry, _ := json.Marshal(map[string]any{"wakeup_id": util.UUIDToString(w.ID), "note": joinedWakeupNote(w, instruction, receipts)})
	task, err := q.JoinQueuedIssueRun(ctx, db.JoinQueuedIssueRunParams{WakeupID: util.UUIDToString(w.ID), Entry: entry, IssueID: issueID, AgentID: agentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", db.AgentTaskQueue{}, nil
	}
	if err != nil {
		return "", db.AgentTaskQueue{}, err
	}
	return wakeupOutcomeMerged, task, nil
}

// JoinedWakeupNotes renders the wakeups that joined a run, for its prompt.
func JoinedWakeupNotes(context []byte) string {
	var stored struct {
		Joined []struct {
			Note string `json:"note"`
		} `json:"wakeup_joined"`
	}
	if json.Unmarshal(context, &stored) != nil {
		return ""
	}
	notes := make([]string, 0, len(stored.Joined))
	for _, j := range stored.Joined {
		if strings.TrimSpace(j.Note) != "" {
			notes = append(notes, strings.TrimSpace(j.Note))
		}
	}
	return strings.Join(notes, "\n\n")
}
