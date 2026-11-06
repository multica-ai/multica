package service

// Opt-in native event consumption. The Issue table IS the durable backlog;
// executions are ordinary agent_task_queue rows claimed by existing Daemons.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const nativeEventBatchSize = 32

// Use Multica's native Task terminal/retry state; do not add another
// business-level failure cooldown or eligibility rule at Issue dispatch.
// The native Multica provider registered for the existing Oracle ACP adapters.
// Both production Mac and VN Oracle runtime IDs use provider=hermes.
const nativeOracleACPProvider = "hermes"

type nativeEventConfig struct {
	Enabled     bool     `json:"enabled"`
	ActorUserID string   `json:"actor_user_id"`
	AgentIDs    []string `json:"agent_ids"`
	// One exact, pre-approved Runtime per configured Agent. A later agent
	// rebind cannot silently redirect Oracle work to a different provider.
	RuntimeIDs []string `json:"runtime_ids"`
}

type nativeEventWorkspace struct {
	id     pgtype.UUID
	config nativeEventConfig
}
type nativeEventTarget struct {
	agent db.Agent
}

func nativeEventUUID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(raw); err != nil || !id.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid event queue UUID")
	}
	return id, nil
}

func parseNativeEventConfig(raw []byte) (nativeEventConfig, bool) {
	var settings struct {
		Events nativeEventConfig `json:"oracle_issue_events"`
	}
	if json.Unmarshal(raw, &settings) != nil {
		return nativeEventConfig{}, false
	}
	c := settings.Events
	if !c.Enabled || c.ActorUserID == "" || len(c.AgentIDs) == 0 || len(c.AgentIDs) > 2 ||
		len(c.RuntimeIDs) != len(c.AgentIDs) {
		return nativeEventConfig{}, false
	}
	if _, err := nativeEventUUID(c.ActorUserID); err != nil {
		return nativeEventConfig{}, false
	}
	agents, runtimes := map[string]bool{}, map[string]bool{}
	for idx, a := range c.AgentIDs {
		rt := c.RuntimeIDs[idx]
		if _, err := nativeEventUUID(a); err != nil || agents[a] {
			return nativeEventConfig{}, false
		}
		if _, err := nativeEventUUID(rt); err != nil || runtimes[rt] {
			return nativeEventConfig{}, false
		}
		agents[a], runtimes[rt] = true, true
	}
	return c, true
}

func (s *IssueWakeupService) nativeEventWorkspaces(ctx context.Context) ([]nativeEventWorkspace, error) {
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,settings FROM workspace
 WHERE settings->'oracle_issue_events'->>'enabled'='true'
 -- Rotate each tick so a busy server never starves the workspace that
 -- sorts after the first 50; no extra cursor/state is stored.
 ORDER BY hashtextextended(id::text, floor(extract(epoch FROM now())/30)::bigint)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []nativeEventWorkspace
	for rows.Next() {
		var id pgtype.UUID
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if cfg, ok := parseNativeEventConfig(raw); ok {
			out = append(out, nativeEventWorkspace{id, cfg})
		}
	}
	return out, rows.Err()
}

// Terminal categories are the only reason an Issue leaves the event pool.
func nativeEventCategoryIsTerminal(category string) bool {
	return category == "done" || category == "closed"
}

// Auto-execution is a subset of the original unfinished Issue event population.
// Respect existing assignee/workflow/Triage; blocked Issues may still be researched
// by an authorized explicit native Run but are never blindly auto-dispatched.
func nativeEventMayRun(issue db.Issue, category string, agent db.Agent) bool {
	return issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" &&
		issue.AssigneeID.Valid && issue.AssigneeID == agent.ID &&
		!issue.TriageState.Valid && (category == "todo" || category == "in_progress")
}

// Reads the full unfinished-event population logically, without inserting
// a shadow event table. Active original Tasks reserve their Issue.
func (s *IssueWakeupService) nativePendingIssues(ctx context.Context, ws pgtype.UUID, targets []nativeEventTarget) ([]pgtype.UUID, error) {
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// Do not change the event population: only select native-executable
	// assignments for this Task refill. Backlog/blocked remain original Issues.
	agentIDs := make([]pgtype.UUID, 0, len(targets))
	for _, target := range targets {
		agentIDs = append(agentIDs, target.agent.ID)
	}
	rows, err := tx.Query(ctx, `SELECT i.id FROM issue i WHERE i.workspace_id=$1
 AND issue_effective_status(i.workspace_id,i.status) IN ('todo','in_progress')
 AND i.assignee_type='agent' AND i.assignee_id=ANY($3::uuid[])
 AND i.triage_state IS NULL
 AND NOT EXISTS (SELECT 1 FROM agent_task_queue t WHERE t.issue_id=i.id
   AND t.status IN ('queued','dispatched','running','deferred','waiting_local_directory'))
 -- Terminal Task failures are consumed by the original retry mechanism;
 -- do not mint a fresh attempt=1 Task after an exhausted/nonretryable failure.
 AND COALESCE((SELECT t.status FROM agent_task_queue t
   WHERE t.issue_id=i.id ORDER BY t.created_at DESC,t.id DESC LIMIT 1),'')<>'failed'
 ORDER BY
   COALESCE((SELECT MAX(t.completed_at) FROM agent_task_queue t WHERE t.issue_id=i.id),to_timestamp(0)) ASC,
   CASE i.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END DESC,
   i.created_at,i.id LIMIT $2::integer`, ws, int32(nativeEventBatchSize), agentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *IssueWakeupService) nativeEventTargets(ctx context.Context, ws nativeEventWorkspace) ([]nativeEventTarget, pgtype.UUID, error) {
	actor, _ := nativeEventUUID(ws.config.ActorUserID)
	var out []nativeEventTarget
	for idx, name := range ws.config.AgentIDs {
		id, _ := nativeEventUUID(name)
		agent, err := s.Tasks.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: ws.id})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, actor, err
		}
		if !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			continue
		}
		expectedRuntime, _ := nativeEventUUID(ws.config.RuntimeIDs[idx])
		if agent.RuntimeID != expectedRuntime {
			continue
		}

		if s.authorize(ctx, s.Tasks.Queries, ws.id, actor, agent) != nil {
			continue
		}
		// Reuse the native Agent readiness contract, not a parallel Oracle policy.
		verdict, readyErr := AgentReadiness(ctx, s.Tasks.runtimeLookup(), agent)
		if readyErr != nil || !verdict.Ready() {
			continue
		}
		// The Oracle runtime owns availability, queue depth and concurrency.
		// No separate slot check, heartbeat threshold, or agent load balancing.
		out = append(out, nativeEventTarget{agent})
	}
	return out, actor, nil
}

// The authoritative lock/Issue status/active Task check and task insert are
// one native database transaction. Source Task identity never comes from an
// untrusted ChatGPT prompt.
func (s *IssueWakeupService) enqueueNativeIssueEvent(ctx context.Context, issueID pgtype.UUID,
	agent db.Agent, actor pgtype.UUID) (db.AgentTaskQueue, bool, error) {
	var zero db.AgentTaskQueue
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return zero, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='50ms'"); err != nil {
		return zero, false, err
	}
	// Recheck workspace owner opt-in under row lock: disable/rebind must
	// either complete first or cancel our unstarted Task after this commit.
	var settings []byte
	if err = tx.QueryRow(ctx, "SELECT settings FROM workspace WHERE id=$1 FOR SHARE",
		agent.WorkspaceID).Scan(&settings); err != nil {
		return zero, false, err
	}
	cfg, enabled := parseNativeEventConfig(settings)
	if !enabled || cfg.ActorUserID != util.UUIDToString(actor) {
		return zero, false, nil
	}
	bound := false
	for idx, configuredAgent := range cfg.AgentIDs {
		if configuredAgent == util.UUIDToString(agent.ID) &&
			cfg.RuntimeIDs[idx] == util.UUIDToString(agent.RuntimeID) {
			bound = true
			break
		}
	}
	if !bound {
		return zero, false, nil
	}
	// Transaction-local advisory lock shared with the native Claim guard.
	var issueLock bool
	if err = tx.QueryRow(ctx,
		"SELECT pg_try_advisory_xact_lock(hashtextextended($1::text, 2746))",
		issueID).Scan(&issueLock); err != nil {
		return zero, false, err
	}
	if !issueLock {
		return zero, false, nil
	}
	var ok bool
	if err = tx.QueryRow(ctx, "SELECT lock_task_owner_rows($1,$2,$3)",
		agent.ID, issueID, agent.RuntimeID).Scan(&ok); err != nil {
		return zero, false, err
	}
	if !ok {
		return zero, false, nil
	}
	// A concurrent Issue close/status update uses NO KEY UPDATE; hold
	// SHARE through both status recheck and Task insert, preventing stale
	// queued work from committing after a terminal change.
	var lockedIssue pgtype.UUID
	if err = tx.QueryRow(ctx, "SELECT id FROM issue WHERE id=$1 FOR SHARE",
		issueID).Scan(&lockedIssue); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, false, nil
		}
		return zero, false, err
	}
	q := s.Tasks.Queries.WithTx(tx)
	issue, err := q.GetIssue(ctx, issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	if issue.WorkspaceID != agent.WorkspaceID {
		return zero, false, nil
	}
	category, err := issuestatus.CategoryWithError(ctx, q, issue.WorkspaceID, issue.Status)
	if err != nil {
		return zero, false, err
	}
	// Custom nonterminal statuses, triage and in-review remain pending events.
	if nativeEventCategoryIsTerminal(category) {
		return zero, false, nil
	}
	if !nativeEventMayRun(issue, category, agent) {
		return zero, false, nil
	}
	// Native FailTask owns its retry child and budget. An exhausted or
	// nonretryable latest failure requires an explicit native rerun/correction;
	// never reset attempt=1 just because the Issue is still unfinished.
	var failedTerminal bool
	err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT t.status='failed'
	  FROM agent_task_queue t WHERE t.issue_id=$1
	  ORDER BY t.created_at DESC,t.id DESC LIMIT 1),false)`, issueID).Scan(&failedTerminal)
	if err != nil {
		return zero, false, err
	}
	if failedTerminal {
		return zero, false, nil
	}
	var occupied bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_task_queue
  WHERE issue_id=$1 AND status IN
  ('queued','dispatched','running','deferred','waiting_local_directory'))`, issueID).Scan(&occupied)
	if err != nil {
		return zero, false, err
	}
	if occupied {
		return zero, false, nil
	}
	overlay := s.Tasks.buildRuntimeMCPOverlay(ctx, actor, agent)
	task, err := q.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		ID: dbid.NewV7(), AgentID: agent.ID, RuntimeID: agent.RuntimeID,
		IssueID: issueID, Priority: priorityToInt(issue.Priority),
		TriggerSummary:   pgtype.Text{String: "Native unfinished-Issue event", Valid: true},
		OriginatorUserID: actor, AccountableUserID: actor,
		OriginatorSource:  pgtype.Text{String: "trigger_owner", Valid: true},
		RuntimeMcpOverlay: overlay.Overlay, RuntimeConnectedApps: overlay.ConnectedApps,
	})
	if err != nil {
		if isDuplicatePendingTaskErr(err) {
			return zero, false, nil
		}
		return zero, false, err
	}
	_, err = tx.Exec(ctx, `UPDATE agent_task_queue
  SET context=COALESCE(context,'{}'::jsonb)||'{"oracle_issue_event":true}'::jsonb
  WHERE id=$1`, task.ID)
	if err != nil {
		if isDuplicatePendingTaskErr(err) {
			return zero, false, nil
		}
		return zero, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, false, err
	}
	s.Tasks.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.Tasks.NotifyTaskEnqueued(ctx, task)
	return task, true, nil
}

func nativeEventRecipient(targets []nativeEventTarget, issue db.Issue) db.Agent {
	// Routing uses existing issue ownership, not a separate slot allocator.
	if issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid {
		for _, target := range targets {
			if target.agent.ID == issue.AssigneeID {
				return target.agent
			}
		}
	}
	return targets[0].agent
}

// After the original Task terminal event, enqueue at most one native successor
// for the same Issue, then existing NotifyTaskEnqueued wakes Oracle. Oracle
// owns the concurrency slots. The leased Tick remains a crash-recovery fallback.
func (s *TaskService) dispatchNativeIssueSuccessor(issueID pgtype.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if s == nil || s.Queries == nil || s.TxStarter == nil || !issueID.Valid {
		return
	}
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	workspace, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return
	}
	cfg, enabled := parseNativeEventConfig(workspace.Settings)
	if !enabled {
		return
	}
	wakeup := IssueWakeupService{Tasks: s}
	targets, actor, err := wakeup.nativeEventTargets(ctx, nativeEventWorkspace{id: issue.WorkspaceID, config: cfg})
	if err != nil || len(targets) == 0 {
		return
	}
	// Reuse the one original transaction that verifies Issue status, active
	// Task dedup, workspace binding and claim ownership; no second eligibility
	// decision, runtime slot count or Task database.
	if _, _, err = wakeup.enqueueNativeIssueEvent(ctx, issueID, nativeEventRecipient(targets, issue), actor); err != nil {
		slog.Warn("native issue continuation enqueue failed; leased Tick will retry",
			"issue_id", util.UUIDToString(issueID), "error", err)
	}
}

// Called from the existing 30s Wakeup scheduler lease, not another worker.
// A terminal Task frees an Issue for selection in the next pass.
func (s *IssueWakeupService) RefillNativeIssueEvents(ctx context.Context) error {
	// Wakeup Tick already has a 15s dispatch budget within the 45s leased Job.
	// Reserve only a bounded additional slice for event refills.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	workspaces, err := s.nativeEventWorkspaces(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, ws := range workspaces {
		if ctx.Err() != nil {
			break
		}

		targets, actor, err := s.nativeEventTargets(ctx, ws)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(targets) == 0 {
			continue
		}
		ids, err := s.nativePendingIssues(ctx, ws.id, targets)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				break
			}

			issue, lookupErr := s.Tasks.Queries.GetIssue(ctx, id)
			if lookupErr != nil {
				errs = append(errs, lookupErr)
				continue
			}
			task, ok, err := s.enqueueNativeIssueEvent(ctx, id, nativeEventRecipient(targets, issue), actor)
			if err != nil {
				slog.Warn("native issue event enqueue failed", "issue", util.UUIDToString(id), "error", err)
				errs = append(errs, err)
			} else if ok {
				slog.Debug("native Issue event queued", "task", util.UUIDToString(task.ID))
			}
		}
	}
	return errors.Join(errs...)
}
