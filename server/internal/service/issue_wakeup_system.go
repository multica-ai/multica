package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The child_done system rule wakes a parent's assignee when a stage of its
// sub-issues closes while a later stage waits, and once more when every
// sub-issue is closed. It is an ordinary issue_wakeup row with system_rule set:
// the shared condition, receipts, runaway protection, activity and run model
// apply. What differs is owned by the platform: it has no fixed agent or
// creator (the target is the parent's assignee when it fires, a squad's leader
// for a squad, an inbox notification for a member), it cannot be deleted or
// edited by agents, and it is evaluated when sub-issues change instead of on a
// polling schedule.
const (
	SystemRuleChildDone = "child_done"
	// MaxSystemWakeupInstruction bounds per-issue and workspace instructions.
	MaxSystemWakeupInstruction = 4000
	// WorkspaceSettingChildDone and WorkspaceSettingChildDoneInstruction hold
	// the rule's workspace default. Only an explicit false turns it off.
	WorkspaceSettingChildDone            = "system_wakeup_child_done"
	WorkspaceSettingChildDoneInstruction = "system_wakeup_child_done_instruction"
	// childChangeHint prompts an evaluation after sub-issues changed. Like
	// other hints it never becomes a run input.
	childChangeHint = "children.changed"
)

var childDoneCondition = json.RawMessage(`{"type":"children_done","each_stage":true}`)

// ChildDoneDefaultInstruction is what the woken assignee is asked to do when
// neither the issue nor the workspace sets an instruction. The trigger facts
// say which stage closed, which one is next and how many were cancelled.
const ChildDoneDefaultInstruction = "Sub-issues of this issue have closed; the trigger facts list each stage and which one is next. " +
	"If a later stage is waiting, check that its dependencies are met, then move its sub-issues out of backlog so they start. " +
	"If a sub-issue in the closed stages was cancelled rather than finished, decide whether its work is still needed before advancing. " +
	"When every sub-issue is closed, bring their results together on this issue and move it forward, or mark it ready for review when nothing remains."

// SystemWakeupDefault is the rule's workspace default: whether it is on and
// the instruction set for the workspace ("" when none).
func SystemWakeupDefault(settings []byte) (bool, string) {
	var values map[string]json.RawMessage
	if json.Unmarshal(settings, &values) != nil {
		return true, ""
	}
	var instruction string
	_ = json.Unmarshal(values[WorkspaceSettingChildDoneInstruction], &instruction)
	return string(values[WorkspaceSettingChildDone]) != "false", strings.TrimSpace(instruction)
}

// ChildDoneInstruction is the instruction the rule's runs receive.
func ChildDoneInstruction(issueInstruction string, settings []byte) string {
	if s := strings.TrimSpace(issueInstruction); s != "" {
		return s
	}
	if _, s := SystemWakeupDefault(settings); s != "" {
		return s
	}
	return ChildDoneDefaultInstruction
}

func systemRuleText(rule string) pgtype.Text { return pgtype.Text{String: rule, Valid: true} }

// baselineChildDone records what already holds for a rule that was just
// turned on, so facts that became true while it was off do not wake anyone.
func baselineChildDone(ctx context.Context, tx pgx.Tx, q *db.Queries, rule db.IssueWakeup) error {
	children, err := loadSubIssues(ctx, tx, q, rule.IssueID, rule.WorkspaceID)
	if err != nil {
		return err
	}
	met, fingerprint, _ := stageProgress(children)
	if !met {
		fingerprint = ""
	}
	return q.SetWakeupConditionState(ctx, db.SetWakeupConditionStateParams{ID: rule.ID, ConditionState: fingerprint})
}

// EnsureChildDoneRule returns the parent's rule, creating it when missing.
// asOpen lists sub-issues whose closing is being processed: the new rule's
// baseline treats them as still open, so the change that led to creating the
// rule still wakes the assignee.
func EnsureChildDoneRule(ctx context.Context, tx pgx.Tx, q *db.Queries, parent db.Issue, asOpen map[pgtype.UUID]bool) (db.IssueWakeup, error) {
	rule, err := q.GetSystemWakeup(ctx, db.GetSystemWakeupParams{IssueID: parent.ID, SystemRule: systemRuleText(SystemRuleChildDone)})
	if !errors.Is(err, pgx.ErrNoRows) {
		return rule, err
	}
	ws, err := q.GetWorkspace(ctx, parent.WorkspaceID)
	if err != nil {
		return rule, err
	}
	enabled, _ := SystemWakeupDefault(ws.Settings)
	children, err := loadSubIssues(ctx, tx, q, parent.ID, parent.WorkspaceID)
	if err != nil {
		return rule, err
	}
	for i := range children {
		if asOpen[children[i].ID] {
			children[i].Closed, children[i].Cancelled = false, false
		}
	}
	met, fingerprint, _ := stageProgress(children)
	if !met {
		fingerprint = ""
	}
	rule, err = q.CreateSystemWakeup(ctx, db.CreateSystemWakeupParams{
		ID: dbid.NewV7(), WorkspaceID: parent.WorkspaceID, IssueID: parent.ID,
		EventTypes: []string{"issue.status_changed"}, Condition: childDoneCondition, ConditionState: fingerprint,
		Enabled: enabled, SystemRule: systemRuleText(SystemRuleChildDone),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return q.GetSystemWakeup(ctx, db.GetSystemWakeupParams{IssueID: parent.ID, SystemRule: systemRuleText(SystemRuleChildDone)})
	}
	return rule, err
}

// ProcessChildEvents handles the sub-issue changes recorded on these parents:
// it creates each parent's child_done rule when missing, hands every
// sub-issue rule a hint, and evaluates them right away. The hints are the
// durable handoff; a rule the immediate pass could not dispatch is picked up
// by the scheduler.
func (s *IssueWakeupService) ProcessChildEvents(ctx context.Context, parentIDs ...pgtype.UUID) error {
	var errs []error
	seen := map[pgtype.UUID]bool{}
	for _, parentID := range parentIDs {
		if !parentID.Valid || seen[parentID] {
			continue
		}
		seen[parentID] = true
		if err := s.processChildEvents(ctx, parentID); err != nil {
			slog.Warn("sub-issue wakeups: processing failed; will retry", "error", err, "parent_id", util.UUIDToString(parentID))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *IssueWakeupService) processChildEvents(ctx context.Context, parentID pgtype.UUID) error {
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.Tasks.Queries.WithTx(tx)
	events, err := q.ClaimChildEvents(ctx, parentID)
	if err != nil || len(events) == 0 {
		return err
	}
	ids := make([]pgtype.UUID, 0, len(events))
	closing := map[pgtype.UUID]bool{}
	kinds := map[string]bool{}
	// The agent runs that made the changes that can satisfy a sub-issue
	// condition (a sub-issue closing, leaving, or changing stage), when every
	// such change came from one. Adding or reopening a sub-issue satisfies
	// nothing, so it does not count.
	var sources []string
	allSourced := true
	for _, e := range events {
		ids = append(ids, e.ID)
		kinds[e.Kind] = true
		if e.Kind == "closed" {
			closing[e.ChildID] = true
		}
		if e.Kind == "attached" || e.Kind == "reopened" {
			continue
		}
		if e.SourceTaskID.Valid {
			sources = append(sources, util.UUIDToString(e.SourceTaskID))
		} else {
			allSourced = false
		}
	}
	finish := func() error {
		if err := q.FinishChildEvents(ctx, ids); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	parent, err := q.GetIssue(ctx, parentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finish()
	}
	if err != nil {
		return err
	}
	active, err := wakeupIssueActive(ctx, q, parent)
	if err != nil {
		return err
	}
	if !active {
		return finish()
	}
	if _, err = EnsureChildDoneRule(ctx, tx, q, parent, closing); err != nil {
		return err
	}
	rules, err := q.ListChildConditionWakeups(ctx, parent.ID)
	if err != nil {
		return err
	}
	changes := make([]string, 0, len(kinds))
	for kind := range kinds {
		changes = append(changes, kind)
	}
	hint := map[string]any{"changes": changes}
	if allSourced && len(sources) > 0 {
		hint["source_task_ids"] = sources
	}
	payload, _ := json.Marshal(hint)
	key := "children:" + util.UUIDToString(events[len(events)-1].ID)
	for _, w := range rules {
		if _, err = q.RecordWakeupReceipt(ctx, db.RecordWakeupReceiptParams{ID: dbid.NewV7(), WakeupID: w.ID, Revision: w.Revision, EventKey: key, EventType: childChangeHint, Payload: payload}); err != nil {
			return err
		}
	}
	if err = finish(); err != nil {
		return err
	}
	// People's and agents' rules first, so the system rule sees their run and
	// joins it instead of starting a second one for the same fact.
	var errs []error
	for _, w := range rules {
		if w.SystemRule.Valid {
			err = s.dispatchSystem(ctx, w)
		} else {
			err = s.dispatch(ctx, w)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("wakeup %s: %w", util.UUIDToString(w.ID), err))
		}
	}
	if len(errs) > 0 {
		slog.Info("sub-issue wakeups: immediate dispatch deferred to the scheduler", "parent_id", util.UUIDToString(parent.ID), "error", errors.Join(errs...))
	}
	return nil
}

// SweepChildEvents processes changes that were recorded but not handled
// right after their write, and drops handled rows after a week.
func (s *IssueWakeupService) SweepChildEvents(ctx context.Context) error {
	parents, err := s.Tasks.Queries.ListStaleChildEventParents(ctx)
	if err != nil {
		return err
	}
	err = s.ProcessChildEvents(ctx, parents...)
	cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, cleanupErr := s.Tasks.Queries.DeleteProcessedChildEvents(cleanupCtx, pgtype.Timestamptz{Time: time.Now().Add(-7 * 24 * time.Hour), Valid: true}); cleanupErr != nil {
		err = errors.Join(err, fmt.Errorf("expire sub-issue events: %w", cleanupErr))
	}
	return err
}

// BackfillChildDoneRules creates the rule on open parents that predate it,
// recording what already holds so nothing fires. It returns how many it
// created; zero means every open parent has its rule.
func (s *IssueWakeupService) BackfillChildDoneRules(ctx context.Context, limit int32) (int, error) {
	parents, err := s.Tasks.Queries.ListParentsWithoutSystemWakeup(ctx, db.ListParentsWithoutSystemWakeupParams{SystemRule: systemRuleText(SystemRuleChildDone), PageLimit: limit})
	if err != nil {
		return 0, err
	}
	created := 0
	for _, parent := range parents {
		if err := s.ensureRule(ctx, parent); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

func (s *IssueWakeupService) ensureRule(ctx context.Context, parent db.Issue) error {
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = EnsureChildDoneRule(ctx, tx, s.Tasks.Queries.WithTx(tx), parent, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// wakeTarget is who a system rule reaches: the parent's agent, its squad's
// leader, or its member assignee. Agent is set only when it can run.
type wakeTarget struct {
	Type    string // agent | squad | member | none
	ID      pgtype.UUID
	Agent   db.Agent
	SquadID pgtype.UUID
}

func (t wakeTarget) key() string {
	return t.Type + ":" + util.UUIDToString(t.ID) + ":" + util.UUIDToString(t.Agent.ID) + ":" + util.UUIDToString(t.Agent.RuntimeID)
}

func resolveWakeTarget(ctx context.Context, q *db.Queries, issue db.Issue) (wakeTarget, error) {
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return wakeTarget{Type: "none"}, nil
	}
	target := wakeTarget{Type: issue.AssigneeType.String, ID: issue.AssigneeID}
	usable := func(a db.Agent) bool { return a.RuntimeID.Valid && !a.ArchivedAt.Valid }
	switch target.Type {
	case "member":
		return target, nil
	case "agent":
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return target, err
		}
		if err == nil && usable(agent) {
			target.Agent = agent
		}
		return target, nil
	case "squad":
		squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return target, nil
		}
		if err != nil {
			return target, err
		}
		target.SquadID = squad.ID
		leader, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: squad.LeaderID, WorkspaceID: issue.WorkspaceID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return target, err
		}
		if err == nil && usable(leader) {
			target.Agent = leader
		}
		return target, nil
	}
	return wakeTarget{Type: "none"}, nil
}

// childDoneFacts summarizes the newest observation for the timeline entry and
// a member's notification: the closed stage (absent when every sub-issue
// closed) and how many sub-issues that covers.
func childDoneFacts(receipts []db.IssueWakeupReceipt) map[string]any {
	facts := map[string]any{"rule": SystemRuleChildDone}
	for i := len(receipts) - 1; i >= 0; i-- {
		if receipts[i].EventType != wakeupConditionEventType {
			continue
		}
		var payload struct {
			Observed struct {
				All    bool         `json:"all"`
				Stage  *int32       `json:"stage"`
				Total  int          `json:"total"`
				Stages []stageCount `json:"stages"`
			} `json:"observed"`
		}
		_ = json.Unmarshal(receipts[i].Payload, &payload)
		facts["total"] = payload.Observed.Total
		if !payload.Observed.All && payload.Observed.Stage != nil {
			facts["stage"] = *payload.Observed.Stage
			for _, sc := range payload.Observed.Stages {
				if sc.Stage == *payload.Observed.Stage {
					facts["total"] = sc.Total
				}
			}
		}
		break
	}
	return facts
}

// dispatchSystem evaluates a system rule and wakes, notifies or records its
// outcome. It mirrors dispatch: the same lock order, receipts, runaway
// protection and timeline entries.
func (s *IssueWakeupService) dispatchSystem(ctx context.Context, prev db.IssueWakeup) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// Resolve the target and optional credentials before taking locks, then
	// confirm the target under them.
	snapshot, err := s.Tasks.Queries.GetIssue(ctx, prev.IssueID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	target, err := resolveWakeTarget(ctx, s.Tasks.Queries, snapshot)
	if err != nil {
		return err
	}
	headSHA := s.Tasks.ResolveIssueReviewSHAParam(ctx, prev.IssueID)
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout = '50ms'"); err != nil {
		return err
	}
	q := s.Tasks.Queries.WithTx(tx)
	if target.Agent.ID.Valid {
		var fenced bool
		if err = tx.QueryRow(ctx, "SELECT lock_task_owner_rows($1,$2,$3)", target.Agent.ID, prev.IssueID, target.Agent.RuntimeID).Scan(&fenced); err != nil {
			return err
		}
		if !fenced {
			return pgx.ErrNoRows
		}
	} else if _, err = q.LockWorkspaceForChatSessionCreate(ctx, prev.WorkspaceID); err != nil {
		return err
	}
	issue, err := q.LockWakeupIssue(ctx, prev.IssueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return qCleanupMissingWakeup(ctx, tx, prev.ID)
	}
	if err != nil {
		return err
	}
	w, err := q.LockIssueWakeup(ctx, prev.ID)
	if err != nil {
		return err
	}
	if w.Revision != prev.Revision {
		return tx.Commit(ctx)
	}
	current, err := resolveWakeTarget(ctx, q, issue)
	if err != nil {
		return err
	}
	if current.key() != target.key() {
		// The assignee changed after the read above; the pending receipts
		// stay for the next pass.
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, "UPDATE issue_wakeup_receipt SET processed_at=now() WHERE wakeup_id=$1 AND revision<>$2 AND processed_at IS NULL", w.ID, w.Revision); err != nil {
		return err
	}
	active, err := wakeupIssueActive(ctx, q, issue)
	if err != nil {
		return err
	}
	// A closed parent or a rule that is off or paused keeps its state; a
	// system rule is never disabled by the platform for a closed issue.
	if !active || !w.Enabled {
		if err = q.DiscardWakeupReceipts(ctx, w.ID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	// A parked parent holds: nothing fires and nothing is marked as seen, so
	// a stage that closed meanwhile wakes the assignee once it leaves backlog.
	if issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status) == "backlog" {
		if _, _, err = consumeConditionHints(ctx, tx, w.ID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		return err
	}
	_, causes, err := consumeConditionHints(ctx, tx, w.ID)
	if err != nil {
		return err
	}
	met, fingerprint, observed, err := evaluateCondition(ctx, tx, q, w)
	if err != nil {
		return err
	}
	state := w.ConditionState
	switch {
	case met && fingerprint != state:
		if err = recordConditionMet(ctx, q, w, observed, causes, now); err != nil {
			return err
		}
		state = fingerprint
	case !met:
		state = ""
	}
	if err = q.SetWakeupConditionState(ctx, db.SetWakeupConditionStateParams{ID: w.ID, ConditionState: state}); err != nil {
		return err
	}
	receipts, err := q.ListPendingWakeupReceipts(ctx, db.ListPendingWakeupReceiptsParams{WakeupID: w.ID, Revision: w.Revision})
	if err != nil {
		return err
	}
	if len(receipts) == 0 {
		return tx.Commit(ctx)
	}
	ids := make([]pgtype.UUID, 0, len(receipts))
	for _, r := range receipts {
		ids = append(ids, r.ID)
	}
	var activities []wakeupActivity
	var inbox []db.InboxItem
	note := func(action string, details map[string]any) error {
		a, err := recordWakeupActivity(ctx, q, w, action, "system", pgtype.UUID{}, details)
		if err == nil {
			activities = append(activities, a)
		}
		return err
	}
	commit := func() error {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		s.publishWakeupActivities(activities...)
		for _, item := range inbox {
			s.publishSystemInbox(item, issue.Status)
		}
		return nil
	}
	facts := childDoneFacts(receipts)
	if current.Type != "none" {
		facts["target_type"], facts["target_id"] = current.Type, util.UUIDToString(current.ID)
	}
	// Recorded without a run: nobody to wake, a member to notify, or an
	// assignee whose agent cannot run.
	if current.Type == "member" || !current.Agent.ID.Valid {
		facts["outcome"] = "none"
		if current.Type == "member" {
			facts["outcome"] = "notified"
			details, _ := json.Marshal(facts)
			item, err := q.CreateInboxItem(ctx, db.CreateInboxItemParams{
				ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, RecipientType: "member", RecipientID: current.ID,
				Type: "children_done", Severity: "info", IssueID: issue.ID, Title: issue.Title,
				ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
			})
			if err != nil {
				return err
			}
			inbox = append(inbox, item)
		}
		if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: ids}); err != nil {
			return err
		}
		if err = note(wakeupActivityTriggered, facts); err != nil {
			return err
		}
		return commit()
	}
	agent := current.Agent
	task, err := q.FindPendingWakeupTask(ctx, util.UUIDToString(w.ID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	taskExists := err == nil
	if taskExists && task.Status == "dispatched" {
		// A claimed prompt is immutable; the new facts wait for the next run.
		return tx.Commit(ctx)
	}
	instruction := w
	if ws, e := q.GetWorkspace(ctx, issue.WorkspaceID); e == nil {
		instruction.Instruction = ChildDoneInstruction(w.Instruction, ws.Settings)
	} else {
		instruction.Instruction = ChildDoneInstruction(w.Instruction, nil)
	}
	noteText, evidence := mergeWakeupEvidence(instruction, task, receipts)
	if taskExists {
		task, err = q.ReplaceWakeupEvidence(ctx, db.ReplaceWakeupEvidenceParams{ID: task.ID, HandoffNote: pgtype.Text{String: noteText, Valid: true}, WakeupEvidence: evidence})
		if err != nil {
			return err
		}
		if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: ids, TaskID: task.ID}); err != nil {
			return err
		}
		return commit()
	}
	// The agent's own unfinished run on the parent closed the stage, or a run
	// of the agent is already waiting to start there: no second run.
	outcome, joined, err := avoidRedundantRun(ctx, q, w, instruction.Instruction, agent.ID, issue.ID, receipts)
	if err != nil {
		return err
	}
	if outcome != "" {
		facts["outcome"] = outcome
		if joined.ID.Valid {
			facts["task_id"] = util.UUIDToString(joined.ID)
		}
		if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: ids, TaskID: joined.ID}); err != nil {
			return err
		}
		if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: true, LastTaskID: joined.ID}); err != nil {
			return err
		}
		if err = note(wakeupActivityTriggered, facts); err != nil {
			return err
		}
		return commit()
	}
	recent, err := q.CountRecentWakeupTasks(ctx, db.CountRecentWakeupTasksParams{WakeupID: util.UUIDToString(w.ID), IssueID: w.IssueID, Since: pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}})
	if err != nil {
		return err
	}
	if recent >= wakeupHourlyRunLimit {
		if err = q.PauseIssueWakeup(ctx, db.PauseIssueWakeupParams{ID: w.ID, PausedReason: pgtype.Text{String: wakeupPausedRate, Valid: true}, BlockRuns: true}); err != nil {
			return err
		}
		if err = q.DiscardWakeupReceipts(ctx, w.ID); err != nil {
			return err
		}
		if err = note(wakeupActivityPaused, map[string]any{"rule": SystemRuleChildDone, "reason": wakeupPausedRate, "limit": wakeupHourlyRunLimit}); err != nil {
			return err
		}
		return commit()
	}
	if err = guardIssueNotInTriage(ctx, q, issue.ID, OriginDerived); err != nil {
		return err
	}
	// The run is accountable to whoever caused the parent to exist, as a run
	// the parent's own assignment would start.
	attr := s.Tasks.attributionForIssueTask(ctx, issue, pgtype.UUID{}, attribution.SourceDelegation, pgtype.UUID{})
	if attr, err = s.Tasks.applyAttributionFallback(ctx, attr, agent); err != nil {
		return err
	}
	overlay := s.Tasks.buildRuntimeMCPOverlay(ctx, attr.UserID, agent)
	source, delegatedFrom, _, _ := attributionCreateParams(attr)
	contextJSON, _ := json.Marshal(map[string]any{"wakeup_id": util.UUIDToString(w.ID), "wakeup_revision": w.Revision, "wakeup_evidence": evidence, "wakeup_system": SystemRuleChildDone})
	task, err = q.CreateWakeupTask(ctx, db.CreateWakeupTaskParams{
		ID: dbid.NewV7(), AgentID: agent.ID, RuntimeID: agent.RuntimeID, IssueID: issue.ID, Priority: priorityToInt(issue.Priority),
		TriggerSummary: pgtype.Text{String: "Wakeup: sub-issues closed", Valid: true}, HandoffNote: pgtype.Text{String: noteText, Valid: true},
		IsLeaderTask: pgtype.Bool{Bool: current.Type == "squad", Valid: current.Type == "squad"}, SquadID: current.SquadID,
		OriginatorUserID: attr.UserID, AccountableUserID: attr.AccountableUserID, OriginatorSource: source, DelegatedFromTaskID: delegatedFrom,
		RuleVersionID: attr.RuleVersionID, TriggerEvidenceKind: pgtype.Text{String: "issue_wakeup", Valid: true}, TriggerEvidenceRefID: w.ID,
		HeadSha: headSHA, WakeupContext: contextJSON, RuntimeMcpOverlay: overlay.Overlay, RuntimeConnectedApps: overlay.ConnectedApps,
	})
	if err != nil {
		return err
	}
	if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: ids, TaskID: task.ID}); err != nil {
		return err
	}
	if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: true, LastTaskID: task.ID}); err != nil {
		return err
	}
	if err = q.CountWakeupFires(ctx, w.ID); err != nil {
		return err
	}
	facts["outcome"], facts["task_id"] = "woke", util.UUIDToString(task.ID)
	if err = note(wakeupActivityTriggered, facts); err != nil {
		return err
	}
	if err = commit(); err != nil {
		return err
	}
	s.Tasks.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.Tasks.NotifyTaskEnqueued(ctx, task)
	return nil
}

func (s *IssueWakeupService) publishSystemInbox(item db.InboxItem, issueStatus string) {
	if s.Tasks == nil || s.Tasks.Bus == nil {
		return
	}
	s.Tasks.Bus.Publish(events.Event{
		Type: protocol.EventInboxNew, WorkspaceID: util.UUIDToString(item.WorkspaceID), ActorType: "system",
		Payload: map[string]any{"item": map[string]any{
			"id": util.UUIDToString(item.ID), "workspace_id": util.UUIDToString(item.WorkspaceID),
			"recipient_type": item.RecipientType, "recipient_id": util.UUIDToString(item.RecipientID),
			"type": item.Type, "severity": item.Severity, "issue_id": util.UUIDToPtr(item.IssueID),
			"title": item.Title, "body": util.TextToPtr(item.Body), "read": item.Read, "archived": item.Archived,
			"created_at": util.TimestampToString(item.CreatedAt), "actor_type": util.TextToPtr(item.ActorType),
			"actor_id": util.UUIDToPtr(item.ActorID), "details": json.RawMessage(item.Details), "issue_status": issueStatus,
		}},
	})
}

// SystemWakeupInput is a person's change to a system rule on one issue.
// Omitted fields keep their value.
type SystemWakeupInput struct {
	Enabled     *bool   `json:"enabled"`
	Instruction *string `json:"instruction"`
}

// UpdateChildDoneRule applies a person's change on one issue. From then on
// the rule no longer follows the workspace default. Turning it off withdraws
// runs that have not started.
func (s *IssueWakeupService) UpdateChildDoneRule(ctx context.Context, issueID pgtype.UUID, in SystemWakeupInput) (db.IssueWakeup, error) {
	var out db.IssueWakeup
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	q := s.Tasks.Queries.WithTx(tx)
	issue, err := q.LockWakeupIssue(ctx, issueID)
	if err != nil {
		return out, err
	}
	rule, err := EnsureChildDoneRule(ctx, tx, q, issue, nil)
	if err != nil {
		return out, err
	}
	if rule, err = q.LockIssueWakeup(ctx, rule.ID); err != nil {
		return out, err
	}
	enabled, instruction := rule.Enabled, rule.Instruction
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.Instruction != nil {
		instruction = strings.TrimSpace(*in.Instruction)
	}
	if len(instruction) > MaxSystemWakeupInstruction {
		return out, fmt.Errorf("%w: instruction must be at most %d bytes", ErrWakeupInput, MaxSystemWakeupInstruction)
	}
	if out, err = q.CustomizeSystemWakeup(ctx, db.CustomizeSystemWakeupParams{ID: rule.ID, Enabled: enabled, Instruction: instruction}); err != nil {
		return out, err
	}
	if enabled && !rule.Enabled {
		if err = baselineChildDone(ctx, tx, q, out); err != nil {
			return out, err
		}
	}
	var cancelled []db.AgentTaskQueue
	if !enabled {
		if err = q.DiscardWakeupReceipts(ctx, rule.ID); err != nil {
			return out, err
		}
		if cancelled, err = q.CancelUnstartedWakeupTasks(ctx, util.UUIDToString(rule.ID)); err != nil {
			return out, err
		}
		if err = q.DropJoinedWakeup(ctx, db.DropJoinedWakeupParams{WakeupID: util.UUIDToString(rule.ID), IssueID: rule.IssueID}); err != nil {
			return out, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	for _, task := range cancelled {
		s.Tasks.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)
	}
	return out, nil
}

// SetChildDoneDefault stores the rule's workspace default and applies it to
// every rule nobody customized. It returns how many rules changed.
func (s *IssueWakeupService) SetChildDoneDefault(ctx context.Context, workspaceID pgtype.UUID, enabled *bool, instruction *string) (int64, error) {
	patch := map[string]any{}
	if enabled != nil {
		patch[WorkspaceSettingChildDone] = *enabled
	}
	if instruction != nil {
		text := strings.TrimSpace(*instruction)
		if len(text) > MaxSystemWakeupInstruction {
			return 0, fmt.Errorf("%w: instruction must be at most %d bytes", ErrWakeupInput, MaxSystemWakeupInstruction)
		}
		patch[WorkspaceSettingChildDoneInstruction] = text
	}
	if len(patch) == 0 {
		return 0, nil
	}
	raw, _ := json.Marshal(patch)
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := s.Tasks.Queries.WithTx(tx)
	if err = q.MergeWorkspaceSettings(ctx, db.MergeWorkspaceSettingsParams{ID: workspaceID, Patch: raw}); err != nil {
		return 0, err
	}
	var changed []db.IssueWakeup
	if enabled != nil {
		if changed, err = q.ApplySystemWakeupDefault(ctx, db.ApplySystemWakeupDefaultParams{WorkspaceID: workspaceID, SystemRule: systemRuleText(SystemRuleChildDone), Enabled: *enabled}); err != nil {
			return 0, err
		}
		for _, rule := range changed {
			if *enabled {
				if err = baselineChildDone(ctx, tx, q, rule); err != nil {
					return 0, err
				}
				continue
			}
			if err = q.DiscardWakeupReceipts(ctx, rule.ID); err != nil {
				return 0, err
			}
		}
	}
	return int64(len(changed)), tx.Commit(ctx)
}
