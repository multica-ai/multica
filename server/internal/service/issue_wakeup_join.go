package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/issuestatus"
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
//   - merged: a run of the agent that runs as the same person the rule's own
//     run would is waiting to start on the issue. The rule keeps its inputs,
//     and when a daemon that renders them claims that run they join it
//     (JoinWaitingWakeups): the instruction and facts ride in its prompt and
//     the firing counts like one that started a run. If that run is
//     cancelled or goes to an older daemon, the rule still has its inputs and
//     starts its own run.
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

// hasWaitingRun reports whether a run of the agent that runs as this person
// is waiting to start on the issue. A firing then keeps its inputs for it.
func hasWaitingRun(ctx context.Context, q *db.Queries, issueID, agentID, runAs pgtype.UUID) (bool, error) {
	if !runAs.Valid {
		return false, nil
	}
	_, err := q.FindWaitingIssueRun(ctx, db.FindWaitingIssueRunParams{IssueID: issueID, AgentID: agentID, OriginatorUserID: runAs})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// childDoneRunAs is who a child_done run for this agent runs as: whoever
// caused the parent to exist, as a run the parent's own assignment would
// start.
func (s *IssueWakeupService) childDoneRunAs(ctx context.Context, issue db.Issue, agent db.Agent) (attribution.Result, error) {
	attr := s.Tasks.attributionForIssueTask(ctx, issue, pgtype.UUID{}, attribution.SourceDelegation, pgtype.UUID{})
	return s.Tasks.applyAttributionFallback(ctx, attr, agent)
}

// joinedWakeupNote is what the run is told about a rule that joined it.
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

type joinedWakeup struct {
	WakeupID string `json:"wakeup_id"`
	Revision int64  `json:"wakeup_revision"`
	Note     string `json:"note"`
}

// JoinWaitingWakeups hands a run being claimed the inputs of the wakeup rules
// that waited for it, and drops what rules turned off or changed since an
// earlier claim of it handed over. It returns the run's context as the claim
// should render it. Call it only for a daemon that renders wakeup_joined. A
// rule that is busy, or no longer allowed to reach this run, keeps its inputs
// and starts its own run later.
func (s *IssueWakeupService) JoinWaitingWakeups(ctx context.Context, task db.AgentTaskQueue) ([]byte, error) {
	if !task.IssueID.Valid || !task.OriginatorUserID.Valid || task.Status != "dispatched" {
		return task.Context, nil
	}
	var stored struct {
		Joined []joinedWakeup `json:"wakeup_joined"`
		Chain  []string       `json:"wakeup_chain"`
	}
	_ = json.Unmarshal(task.Context, &stored)
	candidates, err := s.Tasks.Queries.ListWaitingWakeups(ctx, db.ListWaitingWakeupsParams{IssueID: task.IssueID, AgentID: task.AgentID})
	if err != nil || (len(candidates) == 0 && len(stored.Joined) == 0) {
		return task.Context, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return task.Context, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout = '50ms'"); err != nil {
		return task.Context, err
	}
	q := s.Tasks.Queries.WithTx(tx)
	changed := false
	joined := make([]joinedWakeup, 0, len(stored.Joined)+len(candidates))
	for _, entry := range stored.Joined {
		id, err := util.ParseUUID(entry.WakeupID)
		if err != nil {
			changed = true
			continue
		}
		w, err := q.LocklessWakeup(ctx, id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return task.Context, err
		}
		if err != nil || w.DisabledAt.Valid || w.Revision != entry.Revision {
			changed = true
			continue
		}
		joined = append(joined, entry)
	}
	issue, err := q.GetIssue(ctx, task.IssueID)
	if err != nil {
		return task.Context, err
	}
	active, err := wakeupIssueActive(ctx, q, issue)
	if err != nil {
		return task.Context, err
	}
	if !active {
		candidates = nil
	}
	var activities []wakeupActivity
	chain := stored.Chain
	for _, id := range candidates {
		w, err := q.TryLockIssueWakeup(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return task.Context, err
		}
		entry, ruleChain, activity, err := s.joinWaitingWakeup(ctx, q, issue, task, w)
		if err != nil {
			return task.Context, err
		}
		if entry == nil {
			continue
		}
		kept := joined[:0]
		for _, e := range joined {
			if e.WakeupID != entry.WakeupID {
				kept = append(kept, e)
			}
		}
		joined = append(kept, *entry)
		chain = append(chain, ruleChain...)
		activities = append(activities, activity...)
		changed = true
	}
	if !changed {
		return task.Context, nil
	}
	if len(chain) > wakeupChainLimit {
		chain = chain[len(chain)-wakeupChainLimit:]
	}
	values := map[string]json.RawMessage{}
	_ = json.Unmarshal(task.Context, &values)
	values["wakeup_joined"], _ = json.Marshal(joined)
	if len(chain) > 0 {
		values["wakeup_chain"], _ = json.Marshal(chain)
	}
	next, _ := json.Marshal(values)
	updated, err := q.SetClaimedTaskContext(ctx, db.SetClaimedTaskContextParams{Context: next, ID: task.ID, DispatchedAt: task.DispatchedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		// A newer claim of this run answers instead.
		return task.Context, nil
	}
	if err != nil {
		return task.Context, err
	}
	if err = tx.Commit(ctx); err != nil {
		return task.Context, err
	}
	s.publishWakeupActivities(activities...)
	return updated.Context, nil
}

// joinWaitingWakeup hands one locked rule's inputs to the run, or returns nil
// when this rule must start its own run (or, for its own dispatch to decide,
// acknowledge the inputs or pause as a loop).
func (s *IssueWakeupService) joinWaitingWakeup(ctx context.Context, q *db.Queries, issue db.Issue, task db.AgentTaskQueue, w db.IssueWakeup) (*joinedWakeup, []string, []wakeupActivity, error) {
	if w.DisabledAt.Valid || w.IssueID != task.IssueID {
		return nil, nil, nil, nil
	}
	if _, err := q.FindPendingWakeupTask(ctx, util.UUIDToString(w.ID)); err == nil || !errors.Is(err, pgx.ErrNoRows) {
		// The rule's own run takes its inputs.
		return nil, nil, nil, err
	}
	pending, err := q.ListPendingWakeupReceipts(ctx, db.ListPendingWakeupReceiptsParams{WakeupID: w.ID, Revision: w.Revision})
	if err != nil {
		return nil, nil, nil, err
	}
	receipts := pending[:0]
	for _, r := range pending {
		if len(w.Condition) == 0 || r.EventType == wakeupConditionEventType || r.EventType == wakeupTimeoutEventType || r.EventType == wakeupManualEventType {
			receipts = append(receipts, r)
		}
	}
	if len(receipts) == 0 {
		return nil, nil, nil, nil
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: task.AgentID, WorkspaceID: w.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	// The run must be one the rule's own run would be: the same agent, run as
	// the same person, who may still use the agent.
	instruction := w.Instruction
	facts := map[string]any{}
	if w.SystemRule.Valid {
		if !w.Enabled || issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status) == "backlog" {
			return nil, nil, nil, nil
		}
		target, err := resolveWakeTarget(ctx, q, issue)
		if err != nil {
			return nil, nil, nil, err
		}
		if target.Agent.ID != task.AgentID {
			return nil, nil, nil, nil
		}
		runAs, err := s.childDoneRunAs(ctx, issue, agent)
		if err != nil || runAs.UserID != task.OriginatorUserID {
			return nil, nil, nil, err
		}
		settings := []byte(nil)
		if ws, err := q.GetWorkspace(ctx, issue.WorkspaceID); err == nil {
			settings = ws.Settings
		}
		instruction = ChildDoneInstruction(w.Instruction, settings)
		facts = childDoneFacts(receipts)
		facts["target_type"], facts["target_id"] = target.Type, util.UUIDToString(target.ID)
	} else {
		if w.AgentID != task.AgentID || w.CreatedBy != task.OriginatorUserID || (w.Mode == "once" && w.LastTaskID.Valid) {
			return nil, nil, nil, nil
		}
		if err := s.authorize(ctx, q, w.WorkspaceID, w.CreatedBy, agent); err != nil {
			if errors.Is(err, ErrWakeupForbidden) {
				err = nil
			}
			return nil, nil, nil, err
		}
		facts = wakeupTriggerDetails(task, receipts)
	}
	if self, err := acknowledgedBySelf(ctx, q, w, task.AgentID, issue.ID, receipts); err != nil || self {
		return nil, nil, nil, err
	}
	chain, loop, err := wakeupChain(ctx, q, w, receipts)
	if err != nil || loop {
		return nil, nil, nil, err
	}
	ids := make([]pgtype.UUID, 0, len(receipts))
	for _, r := range receipts {
		ids = append(ids, r.ID)
	}
	if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: ids, TaskID: task.ID}); err != nil {
		return nil, nil, nil, err
	}
	enabled, next := w.Enabled, w.NextFireAt
	if w.Mode == "once" {
		enabled, next = false, pgtype.Timestamptz{}
	}
	if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: enabled, NextFireAt: next, LastTaskID: task.ID}); err != nil {
		return nil, nil, nil, err
	}
	if err = q.CountWakeupFires(ctx, w.ID); err != nil {
		return nil, nil, nil, err
	}
	var activities []wakeupActivity
	note := func(action string, details map[string]any) error {
		a, err := recordWakeupActivity(ctx, q, w, action, "system", pgtype.UUID{}, details)
		if err == nil {
			activities = append(activities, a)
		}
		return err
	}
	facts["outcome"], facts["task_id"] = wakeupOutcomeMerged, util.UUIDToString(task.ID)
	if err = note(wakeupActivityTriggered, facts); err != nil {
		return nil, nil, nil, err
	}
	// Joining counts toward the cap like starting a run.
	if enabled && !w.SystemRule.Valid && w.MaxFires.Valid && w.FireCount+1 >= w.MaxFires.Int32 {
		if err = q.PauseIssueWakeup(ctx, db.PauseIssueWakeupParams{ID: w.ID, PausedReason: pgtype.Text{String: wakeupPausedMaxFires, Valid: true}, BlockRuns: false}); err != nil {
			return nil, nil, nil, err
		}
		if err = note(wakeupActivityPaused, map[string]any{"reason": wakeupPausedMaxFires, "limit": w.MaxFires.Int32}); err != nil {
			return nil, nil, nil, err
		}
	}
	entry := &joinedWakeup{WakeupID: util.UUIDToString(w.ID), Revision: w.Revision, Note: joinedWakeupNote(w, instruction, receipts)}
	return entry, chain, activities, nil
}

// JoinedWakeupNotes renders the wakeups that joined a run, for its prompt.
func JoinedWakeupNotes(context []byte) string {
	var stored struct {
		Joined []joinedWakeup `json:"wakeup_joined"`
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
