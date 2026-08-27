package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/chattitle"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

type TaskService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Hub       *realtime.Hub
	Bus       *events.Bus
	Analytics analytics.Client
	Metrics   *obsmetrics.BusinessMetrics
	Wakeup    TaskWakeupNotifier
	// Entitlements supplies Cloud's workspace-scoped issue-count instruction.
	// Nil keeps self-hosted and isolated test services unlimited.
	Entitlements entitlement.Provider
	// SourceContextStorage is used only by the bounded 30-day cleanup pass for
	// terminal quick-create captures. Nil disables it where storage is absent.
	SourceContextStorage SourceContextObjectStore
	// FeatureFlags is the server-side toggle router. Nil is valid and returns
	// each call site's default.
	FeatureFlags *featureflag.Service
	// EmptyClaim caches "this runtime has no queued task" so the daemon
	// poll path can skip a Postgres scan on the steady-state empty case.
	// Optional — a nil cache disables the fast path and every claim
	// goes through the DB. Wired in router.go from the shared Redis
	// client.
	EmptyClaim *EmptyClaimCache
	// ReclaimCheck schedules the next time a runtime can plausibly contain a
	// stale dispatched task. It removes the unconditional reclaim UPDATE from
	// idle claim polls while missing/error states preserve the DB fallback.
	ReclaimCheck *ReclaimCheckCache
	// Composio computes the per-task MCP overlay (Stage 3 of the Composio
	// epic, MUL-3721) — the integration's "current user's connected apps
	// → MCP session URL" hook called from each Enqueue* path. Optional: a
	// nil ComposioOverlayBuilder turns the overlay step into a no-op so
	// every Multica deployment that hasn't enabled Composio behaves
	// exactly as before. Wired in router.go after composiointeg.NewService
	// succeeds; the concrete type is *composio.Service.
	Composio ComposioOverlayBuilder
	// QuickActions generates chat follow-up suggestions through the
	// server-internal LLM layer. Optional: nil (or a disabled client) turns the
	// whole feature off — no pending marker, no pills — which is the expected
	// state for a self-hosted deployment with no MULTICA_LLM_* configuration.
	// Wired in router.go from the same *llm.Client that backs chat auto-titling.
	QuickActions ChatQuickActionsLLM
	// quickActionsInFlight (chat session id -> struct{}{}) and
	// quickActionsRunning admit suggestion passes: one per session, and a
	// process-wide ceiling. Both zero values are usable, so a TaskService built
	// without NewTaskService still gates correctly rather than deadlocking or
	// shedding everything.
	quickActionsInFlight sync.Map
	quickActionsRunning  atomic.Int64

	analyticsContextMu    sync.Mutex
	analyticsContextCache map[string]analytics.TaskContext
	analyticsContextOrder []string
}

type SourceContextObjectStore interface {
	DeleteObject(ctx context.Context, key string) error
	KeyFromURL(rawURL string) string
}

// ComposioOverlayBuilder is the seam TaskService uses to build the per-task
// MCP overlay at enqueue time. Implemented by
// internal/integrations/composio.Service.BuildTaskOverlay; tests provide an
// inline fake so they don't have to spin a fake Composio SDK.
//
// Contract: a zero MCPOverlayResult means "no overlay for this run" — covers
// all gates the implementation enforces (no owner / empty allowlist / empty
// intersection with active connections / empty session URL). Any non-empty
// MCPOverlay is the exact value to store in agent_task_queue.runtime_mcp_overlay;
// ConnectedApps is non-secret metadata to store alongside it for daemon brief
// injection. A non-nil error is surfaced to the caller but treated as
// best-effort — failed overlay computation must not fail the enqueue.
//
// agent is passed by value so the builder can inspect OwnerID and
// ComposioToolkitAllowlist without re-querying the DB; every enqueue path
// already loaded the agent for runtime/archive checks, so passing it is
// free and avoids a second GetAgent round-trip in the hot path.
type ComposioOverlayBuilder interface {
	BuildTaskOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (runtimeapps.MCPOverlayResult, error)
}

type TaskWakeupNotifier interface {
	NotifyTaskAvailable(runtimeID, taskID string)
}

// triggerSummaryMaxLen caps the snapshot length so the row stays cheap to
// transmit (it ends up in every task list response). 200 is enough for a
// recognisable preview of a one-paragraph comment.
const triggerSummaryMaxLen = 200

// truncateForSummary returns s shortened to maxRunes, with a trailing
// `…` when truncated. Operates on runes (not bytes) so multibyte characters
// — Chinese / emoji — count as one each. Strips surrounding whitespace
// first so a leading newline doesn't waste budget.
func truncateForSummary(s string, maxRunes int) string {
	// strings.Builder + Grow avoids the O(N²) realloc cycle of `+=` in
	// a loop. Grow uses byte length, which is an upper bound for the
	// rune-equivalent output (replacing \n/\r/\t with space is byte-equal
	// for ASCII whitespace), so we never reallocate.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	rs := []rune(strings.TrimSpace(b.String()))
	if len(rs) <= maxRunes {
		return string(rs)
	}
	return string(rs[:maxRunes]) + "…"
}

// maxSynthesizedFallbackCommentRunes bounds the completion-fallback comment that
// CompleteTask synthesizes from a task's final output when the agent left no
// comment of its own during the run. A real final assistant message is at most
// a few thousand words; anything larger is a runaway raw-stream dump — every
// streamed text delta concatenated together plus a literal `tool call` line per
// tool_use event — which some runtimes/providers emit as the task's Output on
// long, tool-heavy runs. Such a dump (observed at 190–264 KB) must never be
// posted, even partially, to the issue thread (GH #5455).
const maxSynthesizedFallbackCommentRunes = 8000

const oversizedFallbackCommentNotice = "This task completed, but its output was too large to post safely. The raw output was not posted. Review the task in this issue's Execution log."

// truncateFallbackCommentBody bounds a synthesized completion-fallback comment
// body. Unlike truncateForSummary (which flattens newlines for a one-line row
// snapshot), it preserves genuine final messages below the cap verbatim. Output
// above the cap is untrusted: the reported failure mode puts process narration
// and tool traces at the head, so retaining any excerpt can expose execution
// details and still discard the final answer. Replace the entire body with a
// fixed notice instead. Callers pass the already-redacted body.
func truncateFallbackCommentBody(body string, maxRunes int) string {
	if utf8.RuneCountInString(body) <= maxRunes {
		return body
	}
	return oversizedFallbackCommentNotice
}

const (
	taskAnalyticsContextCacheMax = 4096
	// RuntimeClaimFreshnessSeconds is the maximum DB heartbeat age accepted by
	// every task release path (deferred promotion, stale-dispatch reclaim, and
	// fresh claim). It must exceed the 60s DB heartbeat flush interval, one ~15s
	// daemon heartbeat, and the ~30s batch scheduler tick. 150s leaves a 45s
	// buffer above that 105s worst-case age. Keep the runtime sweeper threshold
	// aligned: a runtime must not start work after it is stale enough to be
	// marked offline.
	RuntimeClaimFreshnessSeconds = 150.0
	// claimResponseRecoveryWindow must exceed daemon client.Timeout for
	// /tasks/claim (30s) plus /tasks/{id}/start (30s) plus scheduling slack.
	// Longer pre-start work is protected by prepareLeaseDuration instead of
	// stretching this global crash-recovery window.
	claimResponseRecoveryWindow = 90 * time.Second
	prepareLeaseDuration        = 45 * time.Second
)

func (s *TaskService) trackTaskForReclaim(task db.AgentTaskQueue, checkAfter time.Time) {
	if !task.RuntimeID.Valid || !task.ID.Valid || task.Status != "dispatched" {
		return
	}
	// The Redis hint is advisory and intentionally uses an application-clock
	// elapsed deadline captured beside the DB command. Comparing PostgreSQL's
	// absolute dispatched_at/lease timestamps with time.Now would mix clocks;
	// PostgreSQL remains authoritative when the reclaim query actually runs.
	// A bounded background context lets this best-effort write outlive a request
	// cancellation without allowing Redis to stall the hot path indefinitely.
	s.ReclaimCheck.Track(
		context.Background(),
		util.UUIDToString(task.RuntimeID),
		util.UUIDToString(task.ID),
		checkAfter,
	)
}

func (s *TaskService) extendTaskReclaimHint(task db.AgentTaskQueue, checkAfter time.Time) {
	if !task.RuntimeID.Valid || !task.ID.Valid || task.Status != "dispatched" {
		return
	}
	// Preserve a later initial recovery deadline; a short prepare lease should
	// only move the hint when repeated extensions actually protect the task for
	// longer. See trackTaskForReclaim for the clock/context rationale.
	s.ReclaimCheck.TrackLater(
		context.Background(),
		util.UUIDToString(task.RuntimeID),
		util.UUIDToString(task.ID),
		checkAfter,
	)
}

func (s *TaskService) forgetTaskReclaim(task db.AgentTaskQueue) {
	if !task.RuntimeID.Valid || !task.ID.Valid {
		return
	}
	// This cleanup is best-effort and uses the cache's bounded timeout; request
	// cancellation must not make a committed task transition leave a stale hint.
	s.ReclaimCheck.Forget(
		context.Background(),
		util.UUIDToString(task.RuntimeID),
		util.UUIDToString(task.ID),
	)
}

// buildCommentTriggerSummary fetches the comment content and truncates
// it for storage on the task row. Returns an invalid pgtype.Text when
// the comment is missing (deleted / wrong workspace / etc) so the column
// stays NULL — front-end falls back to a structural label in that case.
//
// workspaceID scopes the fetch to the task's own workspace: the summary is
// later returned in claim / task-history responses, so a foreign comment UUID
// reaching an enqueue/merge path must NOT leak another workspace's text even in
// truncated form (MUL-4252).
func (s *TaskService) buildCommentTriggerSummary(ctx context.Context, workspaceID, commentID pgtype.UUID) pgtype.Text {
	if !commentID.Valid {
		return pgtype.Text{}
	}
	comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID:          commentID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return pgtype.Text{}
	}
	summary := truncateForSummary(comment.Content, triggerSummaryMaxLen)
	if summary == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: summary, Valid: true}
}

// ResolveOriginatorFromTriggerComment is the exported wrapper used by the
// comment-merge path (MUL-4195) to compute the top-of-chain human originator
// for a newly-arrived comment, so a merge can be gated on the originator being
// unchanged. workspaceID scopes the comment lookup to the task's workspace
// (MUL-4252). See resolveOriginatorFromTriggerComment for the chain rules.
func (s *TaskService) ResolveOriginatorFromTriggerComment(ctx context.Context, workspaceID, commentID pgtype.UUID) pgtype.UUID {
	return s.resolveOriginatorFromTriggerComment(ctx, workspaceID, commentID)
}

// AttributionForMergedComment resolves the FULL attribution snapshot for a comment
// being coalesced into an already-queued task (MUL-4302). A merge re-attributes the
// run to the newly-arrived comment's human, so the whole snapshot — source, evidence,
// delegation lineage, and both person columns — must move together as one
// attribution.Result; re-stamping only the person columns would leave a run showing
// B accountable while still pointing at A's stale source / evidence / level. isMention
// picks the agent-authored label (delegation for a mention / thread-parent, otherwise
// comment_source), matching the fresh-enqueue routing.
//
// The merge re-opens the same fail-closed decision the original enqueue faced: a merge
// swaps the effective trigger, responsible human, and evidence to the NEW comment, so
// "the enqueue already checked" does not carry over. It runs the comment through
// applyAttributionFallback — the identical fail-closed gate the fresh-enqueue path uses
// — and returns ErrAttributionFailClosed when the new comment cannot be attributed
// precisely and the workspace forbids the owner_fallback degrade. The caller must then
// REFUSE the merge and keep the original (precisely-attributed) task snapshot rather
// than re-stamp a queued run to a degraded owner_fallback (Elon must-fix).
func (s *TaskService) AttributionForMergedComment(ctx context.Context, workspaceID, commentID pgtype.UUID, isMention bool, agent db.Agent) (attribution.Result, error) {
	agentAuthoredSource := attribution.SourceCommentSource
	if isMention {
		agentAuthoredSource = attribution.SourceDelegation
	}
	attr := s.attributionFromTriggerComment(ctx, workspaceID, commentID, agentAuthoredSource)
	return s.applyAttributionFallback(ctx, attr, agent)
}

// BuildCommentTriggerSummary is the exported wrapper used by the comment-merge
// path (MUL-4195) to refresh a coalesced task's trigger_summary to the newest
// trigger comment's snapshot. workspaceID scopes the lookup (MUL-4252).
func (s *TaskService) BuildCommentTriggerSummary(ctx context.Context, workspaceID, commentID pgtype.UUID) pgtype.Text {
	return s.buildCommentTriggerSummary(ctx, workspaceID, commentID)
}

// BuildRuntimeMCPOverlayForMerge recomputes the Composio MCP overlay +
// connected-app metadata for (originatorUserID, agent), used when a merge
// re-stamps a coalesced task's originator (MUL-4195 review must-fix #1). The
// overlay is a pure function of (originator, agent); re-stamping it alongside
// originator_user_id keeps the coalescing run's connected-app capabilities and
// audit attribution consistent with the latest trigger comment's originator
// instead of the task's original one. Fails soft to empty (same as the enqueue
// path) so a transient Composio hiccup never blocks the merge.
func (s *TaskService) BuildRuntimeMCPOverlayForMerge(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (overlay, connectedApps []byte) {
	data := s.buildRuntimeMCPOverlay(ctx, originatorUserID, agent)
	return data.Overlay, data.ConnectedApps
}

func NewTaskService(q *db.Queries, tx TxStarter, hub *realtime.Hub, bus *events.Bus, wakeups ...TaskWakeupNotifier) *TaskService {
	var wakeup TaskWakeupNotifier
	if len(wakeups) > 0 {
		wakeup = wakeups[0]
	}
	return &TaskService{Queries: q, TxStarter: tx, Hub: hub, Bus: bus, Wakeup: wakeup}
}

var trivialDoneMarkers = []string{
	"done",
	"готово",
	"готова",
	"сделано",
	"完成",
	"完了",
}

func isTrivialDoneOutput(output string) bool {
	normalized := strings.TrimSpace(strings.ToLower(output))
	normalized = strings.Trim(normalized, ".!！。… ")
	for _, marker := range trivialDoneMarkers {
		if normalized == marker {
			return true
		}
	}
	return false
}

func (s *TaskService) captureTaskQueued(ctx context.Context, task db.AgentTaskQueue) {
	if s.Metrics != nil {
		source, runtimeMode, _ := s.taskMetricsContext(ctx, task)
		s.Metrics.RecordTaskEnqueued(source, runtimeMode)
	}
}

type runtimeMCPOverlayData struct {
	Overlay       json.RawMessage
	ConnectedApps json.RawMessage
}

// buildRuntimeMCPOverlay computes the optional per-task Composio MCP overlay.
// Enqueue paths call this BEFORE inserting the queued row so the daemon cannot
// claim a task during the network round-trip to Composio and miss the overlay.
func (s *TaskService) buildRuntimeMCPOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) runtimeMCPOverlayData {
	if s == nil || s.Composio == nil {
		return runtimeMCPOverlayData{}
	}
	if !featureflags.ComposioMCPAppsEnabled(ctx, s.FeatureFlags) {
		return runtimeMCPOverlayData{}
	}
	result, err := s.Composio.BuildTaskOverlay(ctx, originatorUserID, agent)
	if err != nil {
		slog.Warn("runtime mcp overlay: BuildTaskOverlay failed; task will run without composio overlay",
			"originator_user_id", util.UUIDToString(originatorUserID),
			"agent_id", util.UUIDToString(agent.ID),
			"error", err,
		)
		return runtimeMCPOverlayData{}
	}
	if len(result.MCPOverlay) == 0 {
		slog.Debug("runtime mcp overlay: no composio overlay for task",
			"originator_user_id", util.UUIDToString(originatorUserID),
			"agent_id", util.UUIDToString(agent.ID),
		)
		return runtimeMCPOverlayData{}
	}
	data := runtimeMCPOverlayData{Overlay: result.MCPOverlay}
	if len(result.ConnectedApps) > 0 {
		raw, err := json.Marshal(result.ConnectedApps)
		if err != nil {
			slog.Warn("runtime mcp overlay: marshal connected app metadata failed",
				"originator_user_id", util.UUIDToString(originatorUserID),
				"agent_id", util.UUIDToString(agent.ID),
				"error", err,
			)
			return data
		}
		data.ConnectedApps = raw
	}
	return data
}

// resolveOriginatorFromTriggerComment returns the top-of-chain HUMAN user
// id for a comment that triggered an Enqueue* path. The chain rules
// (MUL-3869):
//
//   - trigger comment authored by a member → originator = author_id (that
//     member IS the top-of-chain human).
//   - trigger comment authored by an agent → read the parent task via
//     comment.source_task_id and inherit its originator_user_id. This is
//     the load-bearing case for agent fan-out: agent A @-mentions agent B,
//     comment author is A, but we MUST surface the human who originally
//     told A to run, not lose the originator at the first agent hop.
//   - missing comment / unknown source task / NULL parent originator →
//     invalid pgtype.UUID. BuildTaskOverlay treats that as "no overlay"
//     (gate 1).
//
// A nil receiver / nil Queries falls through to invalid so unit-test
// setups that don't wire a DB stay safe. workspaceID scopes the comment lookup
// to the task's workspace so a foreign comment UUID cannot resolve an
// originator from another tenant (MUL-4252).
func (s *TaskService) resolveOriginatorFromTriggerComment(ctx context.Context, workspaceID, commentID pgtype.UUID) pgtype.UUID {
	// The originator VALUE is independent of the agent-authored source label, so
	// any label works here; comment_source is passed only as a placeholder.
	return s.attributionFromTriggerComment(ctx, workspaceID, commentID, attribution.SourceCommentSource).UserID
}

// attributionFromTriggerComment resolves the full attribution (accountable
// human + provenance label + delegation lineage + evidence) for a
// comment-triggered run. It performs the DB reads and hands the gathered facts
// to the pure attribution.ClassifyComment rules so the classification stays
// side-effect-free and unit-tested. The returned UserID is byte-identical to
// the pre-MUL-4302 originator resolution, so authorization behavior (Composio
// overlay, canInvokeAgent A2A gate) is unchanged. workspaceID scopes the comment
// lookup to the task's workspace (MUL-4252).
//
// agentAuthoredSource selects the label for an agent-authored trigger comment:
// attribution.SourceCommentSource for the issue-assignee-reacting path,
// attribution.SourceDelegation for an explicit mention / thread-parent /
// squad-leader path.
func (s *TaskService) attributionFromTriggerComment(ctx context.Context, workspaceID, commentID pgtype.UUID, agentAuthoredSource attribution.Source) attribution.Result {
	if s == nil || s.Queries == nil || !commentID.Valid {
		return attribution.Result{Source: attribution.SourceUnattributed}
	}
	comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID:          commentID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return attribution.Result{Source: attribution.SourceUnattributed}
	}
	return s.attributionFromComment(ctx, comment, agentAuthoredSource)
}

// attributionFromComment classifies a run from an already-loaded trigger comment,
// so a caller that already has the row (e.g. to inspect author_type) does not
// re-read it. Kept byte-identical to the inline logic attributionFromTriggerComment
// used before, so authorization behavior is unchanged.
func (s *TaskService) attributionFromComment(ctx context.Context, comment db.Comment, agentAuthoredSource attribution.Source) attribution.Result {
	facts := attribution.CommentFacts{
		CommentID:  comment.ID,
		AuthorType: comment.AuthorType,
		AuthorID:   comment.AuthorID,
	}
	// For an agent-authored comment, walk comment.source_task_id → parent task →
	// parent.originator_user_id (set by every agent comment-write path since
	// migration 120). A NULL/missing source task leaves ParentOriginator
	// invalid, which ClassifyComment maps to unattributed.
	if comment.AuthorType == "agent" && comment.SourceTaskID.Valid {
		facts.SourceTaskID = comment.SourceTaskID
		if parent, err := s.Queries.GetAgentTask(ctx, comment.SourceTaskID); err == nil {
			facts.ParentOriginator = parent.OriginatorUserID
			facts.ParentAccountable = parent.AccountableUserID
		}
	}
	return attribution.ClassifyComment(facts, agentAuthoredSource)
}

// resolveOriginatorForIssueTask returns the top-of-chain human for issue-backed
// dispatches. Comment-triggered runs keep the existing comment-chain semantics;
// direct issue assignment/creation falls back to the issue's member creator.
// Agent-created issues that carry an explicit task-origin link — quick_create
// (daemon quick-create flow) or agent_create (an agent's ordinary `issue
// create`, MUL-4305) — inherit that origin task's originator, since origin_id
// points at the agent_task_queue row that created the issue. Other
// agent/system origins, including autopilot, deliberately remain unattributed.
func (s *TaskService) resolveOriginatorForIssueTask(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID) pgtype.UUID {
	return s.attributionForIssueTask(ctx, issue, triggerCommentID, attribution.SourceCommentSource, pgtype.UUID{}).UserID
}

// attributionForIssueTask resolves the full attribution for an issue-backed
// enqueue. Comment-triggered runs keep the comment-chain semantics; direct
// assignment/creation falls back to the issue's member creator; agent-created
// quick-create issues inherit the origin task's human as a delegation. The
// accountable-human value is byte-identical to resolveOriginatorForIssueTask,
// which now delegates here — so there is a single source of truth and
// authorization is unaffected. agentAuthoredSource labels the agent-authored
// trigger comment case (see attributionFromTriggerComment).
func (s *TaskService) attributionForIssueTask(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, agentAuthoredSource attribution.Source, actorUserID pgtype.UUID) attribution.Result {
	// A direct member action is the accountable human AND originator, ahead of any
	// trigger comment, origin, or rule (MUL-4302 §4/§5). This covers assign/promote,
	// a manual autopilot trigger, and a manual rerun — the last of which may INHERIT
	// a triggerCommentID for the daemon's prompt context, but must still attribute to
	// the member who clicked rerun, not the original comment's human. So the actor is
	// checked before the trigger-comment / origin branches.
	if actorUserID.Valid {
		return attribution.ClassifyDirect(attribution.DirectFacts{IssueID: issue.ID, ActorUserID: actorUserID})
	}
	if triggerCommentID.Valid {
		if s == nil || s.Queries == nil {
			return attribution.Result{Source: attribution.SourceUnattributed}
		}
		// workspace-scoped so a foreign comment UUID cannot resolve a human from
		// another tenant (MUL-4252).
		comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
			ID:          triggerCommentID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return attribution.Result{Source: attribution.SourceUnattributed}
		}
		// A member/agent trigger comment resolves the human (direct_human / delegation
		// / comment_source). A SYSTEM-authored comment — today the Stage-completion
		// child-done comment (issue_child_done.go), which wakes the parent assignee
		// and threads no actor — carries no human and is not part of any delegation
		// chain. Classifying it would degrade straight to owner_fallback (the agent's
		// own owner), which is wrong for a Stage cascade: the woken run should be
		// accountable to whoever caused the PARENT issue to exist. So for a system
		// comment we skip the comment branch and fall through to the parent issue's
		// own provenance below — the same creator / agent_create-origin /
		// autopilot-origin chain a direct enqueue resolves — reaching owner_fallback
		// only if that provenance itself has no human (MUL-4302; raised by Bohan on
		// the stage-cascade fallback).
		if comment.AuthorType != "system" {
			return s.attributionFromComment(ctx, comment, agentAuthoredSource)
		}
	}
	// Autopilot-origin issues (origin_id is the autopilot id) from a schedule /
	// webhook trigger: no human authorized the run, so originator stays NULL, but it
	// is accountable to the human currently RESPONSIBLE for the firing trigger's
	// effective config (creator, then last substantive editor) — trigger_owner
	// (MUL-4302; Elon must-fix), degrading to the rule publisher when no such member
	// is recoverable. Resolved the same way run_only dispatch resolves
	// it, so both autopilot execution modes attribute identically. (A manual trigger
	// carries an actor and is already handled above.) The issue only stores the
	// autopilot id, so bridge issue → active run → trigger_id to find the trigger.
	if s != nil && s.Queries != nil && issue.OriginType.Valid &&
		issue.OriginType.String == "autopilot" && issue.OriginID.Valid {
		var triggerID pgtype.UUID
		if run, err := s.Queries.GetAutopilotRunByIssue(ctx, issue.ID); err == nil {
			triggerID = run.TriggerID
		}
		return triggerOwnerAttribution(ctx, s.Queries, triggerID, issue.WorkspaceID, issue.OriginID, attribution.EvidenceIssueAssignment, issue.ID)
	}
	facts := attribution.DirectFacts{
		IssueID:     issue.ID,
		CreatorType: issue.CreatorType,
		CreatorID:   issue.CreatorID,
	}
	// Member-created issues resolve without a DB read. Only origin-linked
	// agent-created issues (quick_create, agent_create) need to load the origin
	// task to inherit its human, and only when the DB is wired (nil Queries keeps
	// unit-test setups safe and yields unattributed). Both origin types stamp
	// origin_id with the agent_task_queue row that created the issue, so the
	// top-of-chain human is that task's originator_user_id (MUL-4305).
	if !(issue.CreatorType == "member" && issue.CreatorID.Valid) &&
		s != nil && s.Queries != nil && issue.OriginType.Valid && issue.OriginID.Valid &&
		(issue.OriginType.String == "quick_create" || issue.OriginType.String == "agent_create") {
		facts.OriginType = issue.OriginType.String
		facts.OriginTaskID = issue.OriginID
		if task, err := s.Queries.GetAgentTask(ctx, issue.OriginID); err == nil {
			facts.OriginOriginator = task.OriginatorUserID
			facts.OriginAccountable = task.AccountableUserID
		}
	}
	return attribution.ClassifyDirect(facts)
}

// ruleOwnerAttribution resolves the rule_owner attribution for an autopilot run
// from its active (latest) rule version snapshot (MUL-4302 §3.4). Shared by both
// autopilot execution modes — run_only dispatch and the create_issue enqueue path —
// so they attribute identically. originator stays NULL (an autopilot carries no
// human's authority); only the audit-accountable side is set, to the version's
// member publisher. A missing version (autopilot published before this feature, or
// none yet) or a non-member/absent publisher degrades to unattributed rather than
// fabricating a human. Never returns an error: attribution must not fail an
// enqueue, and a degraded label is the honest fallback.
func ruleOwnerAttribution(ctx context.Context, q *db.Queries, workspaceID, autopilotID pgtype.UUID, evidenceKind attribution.EvidenceKind, evidenceRefID pgtype.UUID) attribution.Result {
	if q == nil || !autopilotID.Valid {
		return attribution.RuleOwner(pgtype.UUID{}, pgtype.UUID{}, evidenceKind, evidenceRefID)
	}
	ver, err := q.GetActiveAutopilotRuleVersion(ctx, db.GetActiveAutopilotRuleVersionParams{
		WorkspaceID: workspaceID,
		AutopilotID: autopilotID,
	})
	if err != nil {
		return attribution.RuleOwner(pgtype.UUID{}, pgtype.UUID{}, evidenceKind, evidenceRefID)
	}
	var publisher pgtype.UUID
	if ver.PublishedByType == "member" {
		publisher = ver.PublishedByID
	}
	return attribution.RuleOwner(publisher, ver.ID, evidenceKind, evidenceRefID)
}

// triggerOwnerAttribution resolves an autopilot schedule/webhook run to the human
// currently RESPONSIBLE for the firing trigger's effective config (MUL-4302; Bohan +
// Elon must-fix). triggerID is the autopilot_run's trigger_id. The trigger row's
// published_by starts at the creator and transfers to whoever later substantively
// edits it, so the run attributes to whoever last shaped what fires it — not the
// original creator. A trigger with no recorded publisher (predating this migration)
// or an agent publisher degrades to ruleOwnerAttribution (rule publisher, then
// owner_fallback) — the same coarser behavior autopilots had before, so nothing
// regresses. Never errors: attribution must not fail an enqueue.
func triggerOwnerAttribution(ctx context.Context, q *db.Queries, triggerID, workspaceID, autopilotID pgtype.UUID, evidenceKind attribution.EvidenceKind, evidenceRefID pgtype.UUID) attribution.Result {
	if q != nil && triggerID.Valid {
		// published_by is the member CURRENTLY responsible for this trigger's
		// effective config: the creator until someone substantively edits it (that
		// trigger's cron/filter/webhook, or an autopilot-level change that bumps all
		// its triggers), then the editor. So a run attributes to whoever last shaped
		// what fires it, not the original creator — and editing another trigger never
		// moves this one (MUL-4302; Elon must-fix).
		if trig, err := q.GetAutopilotTrigger(ctx, triggerID); err == nil &&
			trig.PublishedByType.Valid && trig.PublishedByType.String == "member" && trig.PublishedByID.Valid {
			return attribution.TriggerOwner(trig.PublishedByID, evidenceKind, evidenceRefID)
		}
	}
	return ruleOwnerAttribution(ctx, q, workspaceID, autopilotID, evidenceKind, evidenceRefID)
}

// ErrAttributionFailClosed signals that a run resolved to no precise accountable
// human and the enqueue is REFUSED rather than started. It covers three cases, all
// of which mean "we cannot guarantee an accountable human for this run" (MUL-4302
// §1/§3.5): the workspace opted into fail-closed; the workspace policy could not be
// read (so we cannot confirm fallback is allowed — fail closed, don't run); or
// owner_fallback has no agent owner to fall back to. Enqueue paths surface it so the
// run never starts.
var ErrAttributionFailClosed = errors.New("attribution: no precise accountable human and enqueue refused (fail-closed policy, policy read failed, or no agent owner)")

// ErrDuplicatePendingTask means a fresh enqueue lost the race to a concurrent
// one: a queued/dispatched task for the same (issue, agent) already exists, so
// the pending-task unique index rejected the insert (#5914). This is a benign
// outcome — a sibling run already covers this target
// — not a server fault. Enqueue paths return it so callers can report a
// success-shaped coalesced outcome / structured 409 instead of surfacing the
// raw Postgres constraint as a 500. It is returned BARE (the raw driver text,
// including the index name, is logged once at debug and never wrapped in) so no
// upper-layer log or response can leak the constraint name (#5914, Elon review).
var ErrDuplicatePendingTask = errors.New("a pending task for this issue and agent already exists")

// isDuplicatePendingTaskErr reports whether err is the pending-task unique-index
// violation (a concurrent enqueue won the race). Accept both names while v1 and
// v2 can coexist during a rolling deploy.
func isDuplicatePendingTaskErr(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	switch pgErr.ConstraintName {
	case "idx_one_pending_task_per_issue_agent", "idx_one_pending_task_per_issue_agent_v2":
		return true
	default:
		return false
	}
}

// pendingSlotTakenErr reports whether err means "the (issue, agent) pending slot
// was already occupied when we tried to enqueue".
//
// Two shapes reach RerunIssue. The issue-assignee path surfaces the raw unique
// violation, while enqueueMentionTaskWithCommentPlan normalizes it into the bare
// ErrDuplicatePendingTask sentinel — and that is the path taken by EVERY rerun
// whose target is not the issue's current agent assignee: a squad leader, a
// displaced agent re-fired by task_id, a mentioned agent. Matching only the raw
// pgconn error meant the reclaim never ran for those, so a system retry winning
// the slot surfaced as a hard error instead.
func pendingSlotTakenErr(err error) bool {
	return isDuplicatePendingTaskErr(err) || errors.Is(err, ErrDuplicatePendingTask)
}

// applyAttributionFallback applies the workspace's degraded-attribution policy to a
// resolved attribution whose source came back unattributed (no precise human). A
// PRECISE attribution passes through untouched (no policy read at all). For an
// unattributed run the accountable-never-null guarantee is enforced fail-closed —
// we never silently enqueue a task that could run with a NULL accountable_user_id:
//
//   - policy read fails (or no workspace) → REFUSE. We cannot confirm the workspace
//     permits fallback, so we do not run an unattributable task on a transient DB
//     hiccup. (Only the rare unattributed path pays this; precise runs never read.)
//   - fail-closed workspace → REFUSE.
//   - otherwise → owner_fallback (accountable = agent owner, audit-only, originator
//     untouched). If there is no valid agent owner, owner_fallback stays
//     unattributed → REFUSE rather than enqueue a NULL-accountable task.
//
// Keeping this at the enqueue boundary (not inside the pure classifiers) means
// owner_fallback needs the agent owner, which every enqueue path has in hand.
func (s *TaskService) applyAttributionFallback(ctx context.Context, attr attribution.Result, agent db.Agent) (attribution.Result, error) {
	if attr.Source != attribution.SourceUnattributed {
		return attr, nil
	}
	if s == nil || s.Queries == nil || !agent.WorkspaceID.Valid {
		return attr, fmt.Errorf("%w: workspace policy unavailable", ErrAttributionFailClosed)
	}
	failClosed, err := s.Queries.GetWorkspaceAttributionFailClosed(ctx, agent.WorkspaceID)
	if err != nil {
		// Cannot confirm the workspace allows fallback → fail closed rather than
		// silently run an unattributable task.
		return attr, fmt.Errorf("%w: policy read failed: %v", ErrAttributionFailClosed, err)
	}
	if failClosed {
		return attr, ErrAttributionFailClosed
	}
	fallback := attribution.OwnerFallback(attr, agent.OwnerID)
	if fallback.Source == attribution.SourceUnattributed {
		// owner_fallback could not resolve an accountable human (no valid agent
		// owner): refuse rather than enqueue a NULL-accountable task.
		return attr, fmt.Errorf("%w: no agent owner to attribute", ErrAttributionFailClosed)
	}
	return fallback, nil
}

// attributionCreateParams maps a resolved attribution onto the CreateAgentTask
// provenance columns. originator_source is always stamped (never NULL for a new
// row); delegation lineage and evidence are stamped only when present.
func attributionCreateParams(attr attribution.Result) (source pgtype.Text, delegatedFrom pgtype.UUID, evidenceKind pgtype.Text, evidenceRef pgtype.UUID) {
	source = pgtype.Text{String: attr.Source.String(), Valid: true}
	delegatedFrom = attr.DelegatedFromTaskID
	evidenceKind = pgtype.Text{String: string(attr.EvidenceKind), Valid: attr.EvidenceKind != ""}
	evidenceRef = attr.EvidenceRefID
	return
}

// OriginatorForIssueTask exposes resolveOriginatorForIssueTask to callers
// outside the service package (the squad-leader access gate in the handler
// layer) so the gate judges the top-of-chain human with the exact same
// resolution the enqueue path persists on the task row. Without a shared entry
// point the gate saw an empty originator for agent-triggered assigns and denied
// private leaders that the write path would have attributed correctly
// (MUL-4305).
func (s *TaskService) OriginatorForIssueTask(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID) pgtype.UUID {
	return s.resolveOriginatorForIssueTask(ctx, issue, triggerCommentID)
}

func (s *TaskService) captureTaskDispatched(ctx context.Context, task db.AgentTaskQueue) {
	if s.Metrics != nil {
		source, runtimeMode, _ := s.taskMetricsContext(ctx, task)
		s.Metrics.RecordTaskDispatched(util.UUIDToString(task.ID), source, runtimeMode, taskQueueWaitSeconds(task))
	}
}

func (s *TaskService) AnalyticsContextForTask(ctx context.Context, task db.AgentTaskQueue) analytics.TaskContext {
	return s.taskAnalyticsContext(ctx, task)
}

func (s *TaskService) captureTaskStarted(ctx context.Context, task db.AgentTaskQueue) {
	if s.Metrics != nil {
		source, runtimeMode, provider := s.taskMetricsContext(ctx, task)
		s.Metrics.RecordTaskStarted(source, runtimeMode, provider)
	}
}

func (s *TaskService) captureTaskCompleted(ctx context.Context, task db.AgentTaskQueue) {
	if s.Metrics != nil {
		source, runtimeMode, _ := s.taskMetricsContext(ctx, task)
		s.Metrics.RecordTaskTerminal(util.UUIDToString(task.ID), source, runtimeMode, task.Status, taskRunSeconds(task), taskTotalSeconds(task), task.Attempt)
	}
}

func (s *TaskService) captureTaskFailed(ctx context.Context, task db.AgentTaskQueue) {
	failureReason := taskFailureReason(task)
	if s.Metrics != nil {
		source, runtimeMode, _ := s.taskMetricsContext(ctx, task)
		s.Metrics.RecordTaskTerminal(util.UUIDToString(task.ID), source, runtimeMode, task.Status, taskRunSeconds(task), taskTotalSeconds(task), task.Attempt)
		s.Metrics.RecordTaskFailed(source, runtimeMode, failureReason)
	}
}

func (s *TaskService) captureTaskCancelled(ctx context.Context, task db.AgentTaskQueue) {
	if s.Metrics != nil {
		source, runtimeMode, _ := s.taskMetricsContext(ctx, task)
		s.Metrics.RecordTaskTerminal(util.UUIDToString(task.ID), source, runtimeMode, task.Status, taskRunSeconds(task), taskTotalSeconds(task), task.Attempt)
	}
	// Revoke any mat_ task tokens minted for this task. Cancellation is
	// a terminal transition, so the running agent process no longer
	// needs to call back; eagerly deleting the token closes the
	// window where a compromised process could keep authenticating
	// against the API until the 24h expiry. Failure is non-fatal — the
	// expiry / FK cascade are the durable guards. MUL-2600.
	if err := s.Queries.DeleteTaskTokensByTask(ctx, task.ID); err != nil {
		slog.Warn("cancel task: failed to revoke task tokens",
			"task_id", util.UUIDToString(task.ID), "error", err)
	}
}

// costUSDTicks is the provider's own price for this usage in 1e-10 USD, or 0
// when it reported none — the metrics layer prefers it over its rate table.
func (s *TaskService) CaptureTaskUsage(ctx context.Context, task db.AgentTaskQueue, provider, model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, costUSDTicks int64) {
	if s.Metrics == nil {
		return
	}
	source, runtimeMode, _ := s.taskMetricsContext(ctx, task)
	s.Metrics.RecordLLMUsage(source, runtimeMode, provider, model, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, costUSDTicks)
}

func (s *TaskService) CaptureQueuedExpiredTasks(ctx context.Context, tasks []db.AgentTaskQueue) {
	if s.Metrics == nil {
		return
	}
	for _, task := range tasks {
		source, runtimeMode, _ := s.taskMetricsContext(ctx, task)
		s.Metrics.RecordTaskQueuedExpired(source, runtimeMode)
	}
}

func (s *TaskService) CaptureLeaseExpiredTasks(ctx context.Context, tasks []db.AgentTaskQueue) {
	if s.Metrics == nil {
		return
	}
	for _, task := range tasks {
		source, _, _ := s.taskMetricsContext(ctx, task)
		s.Metrics.RecordTaskLeaseExpired(source)
	}
}

func (s *TaskService) cachedTaskAnalyticsContext(task db.AgentTaskQueue) (analytics.TaskContext, bool) {
	key := taskAnalyticsContextKey(task)
	if key == "" {
		return analytics.TaskContext{}, false
	}
	s.analyticsContextMu.Lock()
	defer s.analyticsContextMu.Unlock()
	if s.analyticsContextCache == nil {
		return analytics.TaskContext{}, false
	}
	tc, ok := s.analyticsContextCache[key]
	return tc, ok
}

func (s *TaskService) storeTaskAnalyticsContext(task db.AgentTaskQueue, tc analytics.TaskContext) {
	if tc.WorkspaceID == "" {
		return
	}
	key := taskAnalyticsContextKey(task)
	if key == "" {
		return
	}
	s.analyticsContextMu.Lock()
	defer s.analyticsContextMu.Unlock()
	if s.analyticsContextCache == nil {
		s.analyticsContextCache = make(map[string]analytics.TaskContext)
	}
	if _, ok := s.analyticsContextCache[key]; !ok {
		s.analyticsContextOrder = append(s.analyticsContextOrder, key)
		if len(s.analyticsContextOrder) > taskAnalyticsContextCacheMax {
			oldest := s.analyticsContextOrder[0]
			s.analyticsContextOrder = s.analyticsContextOrder[1:]
			delete(s.analyticsContextCache, oldest)
		}
	}
	s.analyticsContextCache[key] = tc
}

func taskAnalyticsContextKey(task db.AgentTaskQueue) string {
	taskID := util.UUIDToString(task.ID)
	if taskID == "" {
		return ""
	}
	return strings.Join([]string{
		taskID,
		util.UUIDToString(task.RuntimeID),
		util.UUIDToString(task.IssueID),
		util.UUIDToString(task.ChatSessionID),
		util.UUIDToString(task.AutopilotRunID),
	}, "|")
}

func (s *TaskService) taskMetricsContext(ctx context.Context, task db.AgentTaskQueue) (source, runtimeMode, provider string) {
	tc := s.taskAnalyticsContext(ctx, task)
	source = "other"
	switch {
	case task.ChatSessionID.Valid:
		source = "chat"
	case task.IssueID.Valid:
		if tc.Source == analytics.SourceAutopilot {
			source = "autopilot_issue"
		} else {
			source = "issue"
		}
	case task.AutopilotRunID.Valid:
		source = "autopilot"
	default:
		if _, ok := s.parseQuickCreateContext(task); ok {
			source = "quick_create"
		} else if tc.Source != "" {
			source = tc.Source
		}
	}
	return source, tc.RuntimeMode, tc.Provider
}

func (s *TaskService) taskAnalyticsContext(ctx context.Context, task db.AgentTaskQueue) analytics.TaskContext {
	if tc, ok := s.cachedTaskAnalyticsContext(task); ok {
		return tc
	}
	tc := analytics.TaskContext{
		AgentID: util.UUIDToString(task.AgentID),
		TaskID:  util.UUIDToString(task.ID),
		Source:  analytics.SourceManual,
	}
	if task.IssueID.Valid {
		tc.IssueID = util.UUIDToString(task.IssueID)
	}
	if task.ChatSessionID.Valid {
		tc.ChatSessionID = util.UUIDToString(task.ChatSessionID)
		tc.Source = analytics.SourceChat
	}
	if task.AutopilotRunID.Valid {
		tc.AutopilotRunID = util.UUIDToString(task.AutopilotRunID)
		tc.Source = analytics.SourceAutopilot
	}

	if task.RuntimeID.Valid {
		if rt, err := s.Queries.GetAgentRuntime(ctx, task.RuntimeID); err == nil {
			tc.WorkspaceID = util.UUIDToString(rt.WorkspaceID)
			tc.RuntimeMode = rt.RuntimeMode
			tc.Provider = rt.Provider
		}
	}
	if tc.WorkspaceID == "" || tc.RuntimeMode == "" {
		if agent, err := s.Queries.GetAgent(ctx, task.AgentID); err == nil {
			if tc.WorkspaceID == "" {
				tc.WorkspaceID = util.UUIDToString(agent.WorkspaceID)
			}
			if tc.RuntimeMode == "" {
				tc.RuntimeMode = agent.RuntimeMode
			}
		}
	}

	if task.IssueID.Valid {
		if issue, err := s.Queries.GetIssue(ctx, task.IssueID); err == nil {
			tc.WorkspaceID = util.UUIDToString(issue.WorkspaceID)
			if issue.CreatorType == "member" {
				tc.UserID = util.UUIDToString(issue.CreatorID)
			}
			if issue.OriginType.Valid {
				switch issue.OriginType.String {
				case "autopilot":
					tc.Source = analytics.SourceAutopilot
					if ap, err := s.Queries.GetAutopilot(ctx, issue.OriginID); err == nil {
						if ap.CreatedByType == "member" {
							tc.UserID = util.UUIDToString(ap.CreatedByID)
						}
					}
				case "quick_create":
					tc.Source = analytics.SourceManual
				}
			}
		}
	}
	if task.ChatSessionID.Valid {
		if cs, err := s.Queries.GetChatSession(ctx, task.ChatSessionID); err == nil {
			tc.WorkspaceID = util.UUIDToString(cs.WorkspaceID)
			tc.UserID = util.UUIDToString(cs.CreatorID)
		}
	}
	if task.AutopilotRunID.Valid {
		if run, err := s.Queries.GetAutopilotRun(ctx, task.AutopilotRunID); err == nil {
			if ap, err := s.Queries.GetAutopilot(ctx, run.AutopilotID); err == nil {
				tc.WorkspaceID = util.UUIDToString(ap.WorkspaceID)
				if ap.CreatedByType == "member" {
					tc.UserID = util.UUIDToString(ap.CreatedByID)
				}
			}
		}
	}
	if qc, ok := s.parseQuickCreateContext(task); ok {
		tc.WorkspaceID = qc.WorkspaceID
		tc.UserID = qc.RequesterID
		tc.Source = analytics.SourceManual
	}
	s.storeTaskAnalyticsContext(task, tc)
	return tc
}

func taskQueueWaitSeconds(task db.AgentTaskQueue) float64 {
	return durationSeconds(task.CreatedAt, task.DispatchedAt)
}

func taskRunSeconds(task db.AgentTaskQueue) float64 {
	return durationSeconds(task.StartedAt, task.CompletedAt)
}

func taskTotalSeconds(task db.AgentTaskQueue) float64 {
	return durationSeconds(task.CreatedAt, task.CompletedAt)
}

func durationSeconds(start, end pgtype.Timestamptz) float64 {
	if !start.Valid || !end.Valid {
		return -1
	}
	seconds := end.Time.Sub(start.Time).Seconds()
	if seconds < 0 {
		return 0
	}
	return seconds
}

func taskFailureReason(task db.AgentTaskQueue) string {
	if task.FailureReason.Valid && task.FailureReason.String != "" {
		return task.FailureReason.String
	}
	return "agent_error"
}

func taskErrorType(reason string) string {
	switch reason {
	case "runtime_offline", "runtime_recovery":
		return "runtime"
	case "timeout", "codex_semantic_inactivity":
		return "timeout"
	case "iteration_limit", "agent_fallback_message":
		return "agent_output"
	case "cancelled", "user_cancelled":
		return "cancelled"
	default:
		return "agent_error"
	}
}

// EnqueueTaskForIssue creates a queued task for an agent-assigned issue.
// No context snapshot is stored — the agent fetches all data it needs at
// runtime via the multica CLI.
func (s *TaskService) EnqueueTaskForIssue(ctx context.Context, issue db.Issue, triggerCommentID ...pgtype.UUID) (db.AgentTaskQueue, error) {
	var commentID pgtype.UUID
	if len(triggerCommentID) > 0 {
		commentID = triggerCommentID[0]
	}
	return s.enqueueIssueTask(ctx, issue, commentID, false, "", pgtype.UUID{}, pgtype.UUID{}, pgtype.Timestamptz{})
}

// EnqueueDeferredChannelIssueTask persists the assigned task for a media-backed
// channel /issue turn without making it claimable yet. The fireAt deadline is a
// crash-safe fallback; the channel router promotes the task as soon as the
// detached attachment transaction settles.
func (s *TaskService) EnqueueDeferredChannelIssueTask(ctx context.Context, issue db.Issue, fireAt time.Time) (db.AgentTaskQueue, error) {
	return s.enqueueIssueTask(ctx, issue, pgtype.UUID{}, false, "", pgtype.UUID{}, pgtype.UUID{}, pgtype.Timestamptz{Time: fireAt, Valid: true})
}

// createDeferredChannelIssueTaskWithQueries inserts the inert media-gated task
// through the caller's query handle. IssueService passes its transaction-bound
// Queries so the issue and task become visible atomically. Composio is
// intentionally absent from the transaction-scoped service: the task cannot be
// claimed while deferred, so the optional external overlay is hydrated after
// commit without holding database locks across a network call.
func (s *TaskService) createDeferredChannelIssueTaskWithQueries(ctx context.Context, q *db.Queries, issue db.Issue, fireAt time.Time) (db.AgentTaskQueue, error) {
	txService := &TaskService{Queries: q}
	return txService.enqueueIssueTask(ctx, issue, pgtype.UUID{}, false, "", pgtype.UUID{}, pgtype.UUID{}, pgtype.Timestamptz{Time: fireAt, Valid: true})
}

// hydrateDeferredChannelIssueTaskOverlay fills the optional Composio overlay
// after the issue+task transaction commits. The conditional update refuses to
// overwrite a comment merge that won the post-commit race and already
// re-attributed the task (with its own matching overlay).
func (s *TaskService) hydrateDeferredChannelIssueTaskOverlay(ctx context.Context, task db.AgentTaskQueue) error {
	if s == nil || s.Queries == nil || s.Composio == nil || !featureflags.ComposioMCPAppsEnabled(ctx, s.FeatureFlags) {
		return nil
	}
	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return fmt.Errorf("load agent for deferred channel issue task overlay: %w", err)
	}
	overlay := s.buildRuntimeMCPOverlay(ctx, task.OriginatorUserID, agent)
	if len(overlay.Overlay) == 0 {
		return nil
	}
	updated, err := s.Queries.SetDeferredChannelIssueTaskRuntimeOverlay(ctx, db.SetDeferredChannelIssueTaskRuntimeOverlayParams{
		ID:                       task.ID,
		RuntimeMcpOverlay:        overlay.Overlay,
		RuntimeConnectedApps:     overlay.ConnectedApps,
		ExpectedOriginatorUserID: task.OriginatorUserID,
	})
	if err != nil {
		return fmt.Errorf("set deferred channel issue task overlay: %w", err)
	}
	if updated == 0 {
		slog.Debug("deferred channel issue task overlay skipped: task plan changed",
			"task_id", util.UUIDToString(task.ID))
	}
	return nil
}

// EnqueueTaskForIssueWithHandoff is the assign/promote variant that carries a
// handoff note into the run's opening context (MUL-3375). The note rides a
// dedicated task column; the daemon renders it via the assignment-handoff
// branch. Empty note behaves exactly like EnqueueTaskForIssue. actorUserID is the
// member who performed the assign/promote and becomes the accountable human for
// the run (MUL-4302 §4); invalid when the caller has no member actor.
func (s *TaskService) EnqueueTaskForIssueWithHandoff(ctx context.Context, issue db.Issue, handoffNote string, actorUserID pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueIssueTask(ctx, issue, pgtype.UUID{}, false, handoffNote, actorUserID, pgtype.UUID{}, pgtype.Timestamptz{})
}

// enqueueIssueTask is the shared implementation behind EnqueueTaskForIssue
// and the manual rerun path. forceFreshSession=true marks the task so the
// daemon claim handler skips the (agent_id, issue_id) resume lookup — the
// user already judged the prior output bad, a fresh agent session is the
// expected behavior.
// ResolveIssueReviewSHA returns the head SHA of the commit currently under
// review for an issue (the head_sha of its most-relevant linked PR), or the
// empty string when the issue has no linked PR. Callers thread this into both
// the reviewer-loop dedup check and the enqueue path so a pending review task
// pinned to an old head does not satisfy a request after HEAD advanced
// (TEN-356). Empty string is the safe default: it makes dedup fall back to the
// pre-TEN-356 (issue_id, agent_id) key and leaves the task's context NULL.
//
// The lookup fails soft — any DB error (including "no linked PR") returns "" so
// a transient github-table hiccup can never over-dedup a review out of
// existence; the worst case is the pre-TEN-356 coalescing behavior.
func (s *TaskService) ResolveIssueReviewSHA(ctx context.Context, issueID pgtype.UUID) string {
	if !issueID.Valid {
		return ""
	}
	sha, err := s.Queries.GetIssueReviewHeadSha(ctx, issueID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("resolve issue review sha failed",
				"issue_id", util.UUIDToString(issueID), "error", err)
		}
		return ""
	}
	return sha
}

// headShaText wraps a resolved review SHA into the pgtype.Text the dedup/enqueue
// queries expect. Empty SHA marshals to an invalid (NULL) Text so the queries
// take their fall-back branch.
func headShaText(sha string) pgtype.Text {
	return pgtype.Text{String: sha, Valid: sha != ""}
}

// ResolveIssueReviewSHAParam is ResolveIssueReviewSHA wrapped as the pgtype.Text
// the dedup queries take, so both service- and handler-package call sites can
// key dedup on the reviewed head with a single call (TEN-356).
func (s *TaskService) ResolveIssueReviewSHAParam(ctx context.Context, issueID pgtype.UUID) pgtype.Text {
	return headShaText(s.ResolveIssueReviewSHA(ctx, issueID))
}

func (s *TaskService) enqueueIssueTask(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfTaskID pgtype.UUID, fireAt pgtype.Timestamptz) (db.AgentTaskQueue, error) {
	return s.enqueueIssueTaskWithCommentPlan(ctx, issue, triggerCommentID, nil, forceFreshSession, handoffNote, actorUserID, rerunOfTaskID, fireAt)
}

func (s *TaskService) enqueueIssueTaskWithCommentPlan(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, coalescedCommentIDs []pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfTaskID pgtype.UUID, fireAt pgtype.Timestamptz) (db.AgentTaskQueue, error) {
	if !issue.AssigneeID.Valid {
		slog.Error("task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "error", "issue has no assignee")
		return db.AgentTaskQueue{}, fmt.Errorf("issue has no assignee")
	}

	agent, err := s.Queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil {
		slog.Error("task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("task enqueue skipped: agent is archived", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agent.ID))
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		slog.Error("task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "error", "agent has no runtime")
		return db.AgentTaskQueue{}, fmt.Errorf("agent has no runtime")
	}

	// The issue assignee reacting to an agent-authored comment is a
	// comment_source attribution (a special case of delegation); a member
	// comment or direct member assignment is direct_human. attr.UserID is the
	// same value the pre-MUL-4302 resolver produced, so overlay/authorization
	// are unchanged; the extra fields are audit provenance.
	attr := s.attributionForIssueTask(ctx, issue, triggerCommentID, attribution.SourceCommentSource, actorUserID)
	// No precise human resolved → owner_fallback (accountable = agent owner), or
	// refuse the enqueue if the workspace is fail-closed (MUL-4302 §3.5).
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		slog.Warn("task enqueue refused: attribution fail-closed", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(issue.AssigneeID))
		return db.AgentTaskQueue{}, err
	}
	originatorUserID := attr.UserID
	runtimeMCPOverlay := s.buildRuntimeMCPOverlay(ctx, originatorUserID, agent)
	attrSource, attrDelegatedFrom, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	createParams := db.CreateAgentTaskParams{
		ID:                   dbid.NewV7(),
		AgentID:              issue.AssigneeID,
		RuntimeID:            agent.RuntimeID,
		IssueID:              issue.ID,
		Priority:             priorityToInt(issue.Priority),
		TriggerCommentID:     triggerCommentID,
		CoalescedCommentIds:  coalescedCommentIDs,
		TriggerSummary:       s.buildCommentTriggerSummary(ctx, issue.WorkspaceID, triggerCommentID),
		ForceFreshSession:    pgtype.Bool{Bool: forceFreshSession, Valid: forceFreshSession},
		HandoffNote:          pgtype.Text{String: handoffNote, Valid: handoffNote != ""},
		OriginatorUserID:     originatorUserID,
		AccountableUserID:    attr.AccountableUserID,
		RuleVersionID:        attr.RuleVersionID,
		RerunOfTaskID:        rerunOfTaskID,
		RuntimeMcpOverlay:    runtimeMCPOverlay.Overlay,
		RuntimeConnectedApps: runtimeMCPOverlay.ConnectedApps,
		OriginatorSource:     attrSource,
		DelegatedFromTaskID:  attrDelegatedFrom,
		TriggerEvidenceKind:  attrEvidenceKind,
		TriggerEvidenceRefID: attrEvidenceRef,
		// Stamp the reviewed head so dedup can distinguish this run's target
		// from a later request against a new HEAD (TEN-356).
		HeadSha: headShaText(s.ResolveIssueReviewSHA(ctx, issue.ID)),
	}
	var task db.AgentTaskQueue
	if fireAt.Valid {
		task, err = s.Queries.CreateDeferredChannelIssueTask(ctx, db.CreateDeferredChannelIssueTaskParams{
			ID:                   dbid.NewV7(),
			AgentID:              createParams.AgentID,
			RuntimeID:            createParams.RuntimeID,
			IssueID:              createParams.IssueID,
			Priority:             createParams.Priority,
			TriggerCommentID:     createParams.TriggerCommentID,
			CoalescedCommentIds:  createParams.CoalescedCommentIds,
			TriggerSummary:       createParams.TriggerSummary,
			ForceFreshSession:    createParams.ForceFreshSession,
			IsLeaderTask:         createParams.IsLeaderTask,
			HandoffNote:          createParams.HandoffNote,
			SquadID:              createParams.SquadID,
			HeadSha:              createParams.HeadSha,
			OriginatorUserID:     createParams.OriginatorUserID,
			AccountableUserID:    createParams.AccountableUserID,
			RuntimeMcpOverlay:    createParams.RuntimeMcpOverlay,
			RuntimeConnectedApps: createParams.RuntimeConnectedApps,
			OriginatorSource:     createParams.OriginatorSource,
			DelegatedFromTaskID:  createParams.DelegatedFromTaskID,
			RuleVersionID:        createParams.RuleVersionID,
			RerunOfTaskID:        createParams.RerunOfTaskID,
			TriggerEvidenceKind:  createParams.TriggerEvidenceKind,
			TriggerEvidenceRefID: createParams.TriggerEvidenceRefID,
			FireAt:               fireAt,
		})
	} else {
		task, err = s.Queries.CreateAgentTask(ctx, createParams)
	}
	if err != nil {
		slog.Error("task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("create task: %w", err)
	}

	slog.Info("task enqueued",
		"task_id", util.UUIDToString(task.ID),
		"issue_id", util.UUIDToString(issue.ID),
		"agent_id", util.UUIDToString(issue.AssigneeID),
		"force_fresh_session", forceFreshSession,
	)
	if fireAt.Valid {
		return task, nil
	}
	// Order matters: broadcast first, notify daemon second. notifyTaskAvailable
	// kicks an in-process channel that the daemon picks up over HTTP and
	// claims; the claim path then emits its own task:dispatch. Doing the
	// queued broadcast afterwards risks the dispatch event reaching clients
	// before the queued one (rare but unsafe-by-construction). Publishing
	// in the desired observe-order makes correctness independent of timing.
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.NotifyTaskEnqueued(ctx, task)
	return task, nil
}

// EnqueueTaskForMention creates a queued task for a mentioned agent on an issue.
// Unlike EnqueueTaskForIssue, this takes an explicit agent ID rather than
// deriving it from the issue assignee.
func (s *TaskService) EnqueueTaskForMention(ctx context.Context, issue db.Issue, agentID pgtype.UUID, triggerCommentID pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueMentionTask(ctx, issue, agentID, triggerCommentID, false, pgtype.UUID{}, false, "", pgtype.UUID{}, pgtype.UUID{})
}

// EnqueueTaskForThreadParent creates a queued task for the agent who authored
// the direct parent comment a member replied to.
func (s *TaskService) EnqueueTaskForThreadParent(ctx context.Context, issue db.Issue, agentID pgtype.UUID, triggerCommentID pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueMentionTask(ctx, issue, agentID, triggerCommentID, false, pgtype.UUID{}, false, "", pgtype.UUID{}, pgtype.UUID{})
}

// EnqueueTaskForSquadLeader is the leader-role variant of EnqueueTaskForMention.
// The resulting task carries is_leader_task=true so that downstream
// self-trigger guards can distinguish a comment posted while the agent was
// acting as the squad's leader (skip) from one posted while it was acting
// as a worker (do not skip). This matters for agents that are simultaneously
// the leader and a worker of the same squad — see migration 090.
//
// squadID is stamped onto the task's squad_id column so the daemon claim
// handler can locate the squad and inject its briefing regardless of how the
// leader task was triggered (comment @squad, issue assign, autopilot,
// sub-issue done callback). See migration 127.
func (s *TaskService) EnqueueTaskForSquadLeader(ctx context.Context, issue db.Issue, leaderID pgtype.UUID, squadID pgtype.UUID, triggerCommentID pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueMentionTask(ctx, issue, leaderID, triggerCommentID, true, squadID, false, "", pgtype.UUID{}, pgtype.UUID{})
}

// EnqueueTaskForSquadLeaderWithHandoff is the assign/promote variant carrying a
// handoff note into the leader run's opening context (MUL-3375). Empty note
// behaves exactly like EnqueueTaskForSquadLeader. actorUserID is the member who
// performed the assign/promote and becomes the accountable human (MUL-4302 §4);
// invalid when the caller has no member actor.
func (s *TaskService) EnqueueTaskForSquadLeaderWithHandoff(ctx context.Context, issue db.Issue, leaderID pgtype.UUID, squadID pgtype.UUID, handoffNote string, actorUserID pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueMentionTask(ctx, issue, leaderID, pgtype.UUID{}, true, squadID, false, handoffNote, actorUserID, pgtype.UUID{})
}

func (s *TaskService) enqueueMentionTask(ctx context.Context, issue db.Issue, agentID pgtype.UUID, triggerCommentID pgtype.UUID, isLeader bool, squadID pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfTaskID pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueMentionTaskWithCommentPlan(ctx, issue, agentID, triggerCommentID, nil, isLeader, squadID, forceFreshSession, handoffNote, actorUserID, rerunOfTaskID)
}

func (s *TaskService) enqueueMentionTaskWithCommentPlan(ctx context.Context, issue db.Issue, agentID pgtype.UUID, triggerCommentID pgtype.UUID, coalescedCommentIDs []pgtype.UUID, isLeader bool, squadID pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfTaskID pgtype.UUID) (db.AgentTaskQueue, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		slog.Error("mention task enqueue failed: agent not found", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("mention task enqueue skipped: agent is archived", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		slog.Error("mention task enqueue failed: agent has no runtime", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentTaskQueue{}, fmt.Errorf("agent has no runtime")
	}

	// An explicit mention / thread-parent / squad-leader hop from an
	// agent-authored comment is a delegation (the parent task's human is
	// copied); a member mention is direct_human. attr.UserID matches the
	// pre-MUL-4302 value, so authorization is unchanged.
	attr := s.attributionForIssueTask(ctx, issue, triggerCommentID, attribution.SourceDelegation, actorUserID)
	// No precise human resolved → owner_fallback (accountable = agent owner), or
	// refuse the enqueue if the workspace is fail-closed (MUL-4302 §3.5).
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		slog.Warn("mention task enqueue refused: attribution fail-closed", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentTaskQueue{}, err
	}
	originatorUserID := attr.UserID
	runtimeMCPOverlay := s.buildRuntimeMCPOverlay(ctx, originatorUserID, agent)
	attrSource, attrDelegatedFrom, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	task, err := s.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		ID:                   dbid.NewV7(),
		AgentID:              agentID,
		RuntimeID:            agent.RuntimeID,
		IssueID:              issue.ID,
		Priority:             priorityToInt(issue.Priority),
		TriggerCommentID:     triggerCommentID,
		CoalescedCommentIds:  coalescedCommentIDs,
		TriggerSummary:       s.buildCommentTriggerSummary(ctx, issue.WorkspaceID, triggerCommentID),
		IsLeaderTask:         pgtype.Bool{Bool: isLeader, Valid: isLeader},
		ForceFreshSession:    pgtype.Bool{Bool: forceFreshSession, Valid: forceFreshSession},
		HandoffNote:          pgtype.Text{String: handoffNote, Valid: handoffNote != ""},
		SquadID:              squadID,
		OriginatorUserID:     originatorUserID,
		AccountableUserID:    attr.AccountableUserID,
		RuleVersionID:        attr.RuleVersionID,
		RerunOfTaskID:        rerunOfTaskID,
		RuntimeMcpOverlay:    runtimeMCPOverlay.Overlay,
		RuntimeConnectedApps: runtimeMCPOverlay.ConnectedApps,
		OriginatorSource:     attrSource,
		DelegatedFromTaskID:  attrDelegatedFrom,
		TriggerEvidenceKind:  attrEvidenceKind,
		TriggerEvidenceRefID: attrEvidenceRef,
		// Stamp the reviewed head so dedup can distinguish this run's target
		// from a later request against a new HEAD (TEN-356).
		HeadSha: headShaText(s.ResolveIssueReviewSHA(ctx, issue.ID)),
	})
	if err != nil {
		// A concurrent enqueue for the same (issue, agent) won the race and the
		// unique index rejected this insert. That is benign — a sibling run
		// already covers this target — so log it at debug and return a typed
		// sentinel the caller maps to a coalesced outcome / 409 rather than a
		// 500 that leaks the raw constraint name (#5914).
		if isDuplicatePendingTaskErr(err) {
			slog.Debug("mention task enqueue coalesced: pending task already exists", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
			return db.AgentTaskQueue{}, ErrDuplicatePendingTask
		}
		slog.Error("mention task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("create task: %w", err)
	}

	slog.Info("mention task enqueued", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID), "is_leader_task", isLeader)
	// See EnqueueTaskForIssue for ordering rationale.
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.NotifyTaskEnqueued(ctx, task)
	return task, nil
}

// EnqueueDeferredAssigneeFallback creates an inert task that becomes claimable
// only after PromoteDueDeferredTasksForRuntime flips it from deferred to queued.
func (s *TaskService) EnqueueDeferredAssigneeFallback(ctx context.Context, issue db.Issue, agentID, squadID pgtype.UUID, escalationForTaskID pgtype.UUID, triggerCommentID pgtype.UUID, fireAt time.Time) (db.AgentTaskQueue, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		slog.Error("deferred fallback enqueue failed: agent not found", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("deferred fallback enqueue skipped: agent is archived", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		slog.Error("deferred fallback enqueue failed: agent has no runtime", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentTaskQueue{}, fmt.Errorf("agent has no runtime")
	}

	// The fallback assignee is reacting to the same trigger comment as the primary
	// routed task, so resolve attribution from that comment (member author →
	// direct_human; agent author → comment_source chain) and stamp it at creation.
	// Promotion later only flips status, so stamping here keeps the eventual run
	// off the NULL-source bypass (MUL-4302 §2). Overlay is intentionally left for
	// the existing promotion path — this change is attribution-only. No direct
	// actor here: the fallback is comment-routed, so attribution rides the comment.
	attr := s.attributionForIssueTask(ctx, issue, triggerCommentID, attribution.SourceCommentSource, pgtype.UUID{})
	// No precise human resolved → owner_fallback (accountable = agent owner), or
	// refuse if the workspace is fail-closed (MUL-4302 §3.5).
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		slog.Warn("deferred fallback enqueue refused: attribution fail-closed", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentTaskQueue{}, err
	}
	attrSource, attrDelegatedFrom, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	isLeader := squadID.Valid
	task, err := s.Queries.CreateDeferredAgentTask(ctx, db.CreateDeferredAgentTaskParams{
		ID:                   dbid.NewV7(),
		AgentID:              agentID,
		RuntimeID:            agent.RuntimeID,
		IssueID:              issue.ID,
		Priority:             priorityToInt(issue.Priority),
		TriggerCommentID:     triggerCommentID,
		TriggerSummary:       s.buildCommentTriggerSummary(ctx, issue.WorkspaceID, triggerCommentID),
		IsLeaderTask:         pgtype.Bool{Bool: isLeader, Valid: isLeader},
		SquadID:              squadID,
		EscalationForTaskID:  escalationForTaskID,
		FireAt:               pgtype.Timestamptz{Time: fireAt, Valid: true},
		OriginatorUserID:     attr.UserID,
		AccountableUserID:    attr.AccountableUserID,
		OriginatorSource:     attrSource,
		DelegatedFromTaskID:  attrDelegatedFrom,
		TriggerEvidenceKind:  attrEvidenceKind,
		TriggerEvidenceRefID: attrEvidenceRef,
	})
	if err != nil {
		slog.Error("deferred fallback enqueue failed", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("create deferred task: %w", err)
	}

	slog.Info("deferred fallback task enqueued",
		"task_id", util.UUIDToString(task.ID),
		"issue_id", util.UUIDToString(issue.ID),
		"agent_id", util.UUIDToString(agentID),
		"fire_at", fireAt.UTC().Format(time.RFC3339),
	)
	return task, nil
}

// QuickCreateContext is the JSON payload stored on a quick-create task's
// context column. The daemon detects this variant via Type == "quick_create"
// and switches to the quick-create prompt template; the completion path
// uses RequesterID + WorkspaceID to write the inbox notification.
//
// ProjectID is the optional project the user picked in the modal. When
// non-empty the daemon claim handler resolves the project's title +
// resources, and the prompt template instructs the agent to pass
// `--project <uuid>` so the new issue lands in that project.
//
// SquadID is non-empty when the user picked a squad (rather than an agent)
// in the modal. The task is still enqueued against the squad's leader
// agent (Queries.CreateQuickCreateTask is agent-scoped); SquadID is the
// hint the daemon claim handler uses to layer the squad-leader briefing
// onto the agent's Instructions, matching the behavior of issue-bound
// tasks assigned to the squad.
type QuickCreateContext struct {
	Type          string   `json:"type"`
	Prompt        string   `json:"prompt"`
	RequesterID   string   `json:"requester_id"`
	WorkspaceID   string   `json:"workspace_id"`
	Priority      string   `json:"priority,omitempty"`
	DueDate       string   `json:"due_date,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	SquadID       string   `json:"squad_id,omitempty"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
	// ParentIssueID is the optional UUID of the parent issue the new issue
	// should be filed under. Set when the user opens the modal from "Add
	// sub issue" on an existing issue; the daemon claim handler resolves the
	// parent's identifier and the prompt template instructs the agent to
	// pass `--parent <uuid>` so the sub-issue relationship is preserved
	// across the manual→agent mode flip.
	ParentIssueID string `json:"parent_issue_id,omitempty"`
	// SourceContextID identifies the immutable pending capture that must attach
	// to the one issue this quick-create chain produces.
	SourceContextID string `json:"source_context_id,omitempty"`
}

// QuickCreateContextType marks a task as a quick-create job.
const QuickCreateContextType = "quick_create"

// EnqueueQuickCreateTask creates a queued task that has no issue / chat /
// autopilot link — the user's natural-language prompt is stored in the
// task's context JSONB and the agent is expected to translate it into a
// `multica issue create` call. Pre-validates that the agent is reachable
// (not archived, has a runtime) so the API can reject up-front rather than
// queue a task no one will ever claim.
//
// projectID is optional (zero-valued pgtype.UUID when the user didn't pick
// one). The handler is responsible for validating it belongs to the same
// workspace before passing it in.
//
// squadID is non-empty (Valid) when the user picked a squad as the actor.
// The handler has already resolved it to the squad's leader agent for
// agentID; the squadID hint is stamped into the task context so the daemon
// claim handler can inject the squad-leader briefing on dispatch.
//
// parentIssueID is optional (zero-valued pgtype.UUID when the user didn't
// open the modal from "Add sub issue"). The handler is responsible for
// validating it belongs to the same workspace before passing it in.
func (s *TaskService) EnqueueQuickCreateTask(ctx context.Context, workspaceID, requesterID pgtype.UUID, agentID, squadID pgtype.UUID, prompt, priority, dueDate string, projectID, parentIssueID pgtype.UUID, attachmentIDs []pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueQuickCreateTask(ctx, workspaceID, requesterID, agentID, squadID, prompt, priority, dueDate, projectID, parentIssueID, attachmentIDs, nil)
}

func (s *TaskService) EnqueueQuickCreateTaskWithSourceContext(ctx context.Context, workspaceID, requesterID pgtype.UUID, agentID, squadID pgtype.UUID, prompt, priority, dueDate string, projectID, parentIssueID pgtype.UUID, attachmentIDs []pgtype.UUID, capture SourceContextCapture) (db.AgentTaskQueue, error) {
	return s.enqueueQuickCreateTask(ctx, workspaceID, requesterID, agentID, squadID, prompt, priority, dueDate, projectID, parentIssueID, attachmentIDs, &capture)
}

func (s *TaskService) enqueueQuickCreateTask(ctx context.Context, workspaceID, requesterID pgtype.UUID, agentID, squadID pgtype.UUID, prompt, priority, dueDate string, projectID, parentIssueID pgtype.UUID, attachmentIDs []pgtype.UUID, capture *SourceContextCapture) (db.AgentTaskQueue, error) {
	if err := CheckIssueCreateCapacity(ctx, s.Queries, s.Entitlements, workspaceID); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("preflight quick-create issue capacity: %w", err)
	}
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("agent has no runtime")
	}

	payload := QuickCreateContext{
		Type:        QuickCreateContextType,
		Prompt:      prompt,
		RequesterID: util.UUIDToString(requesterID),
		WorkspaceID: util.UUIDToString(workspaceID),
		Priority:    priority,
		DueDate:     dueDate,
	}
	if projectID.Valid {
		payload.ProjectID = util.UUIDToString(projectID)
	}
	if squadID.Valid {
		payload.SquadID = util.UUIDToString(squadID)
	}
	if parentIssueID.Valid {
		payload.ParentIssueID = util.UUIDToString(parentIssueID)
	}
	if capture != nil {
		payload.SourceContextID = util.UUIDToString(capture.ID)
	}
	if len(attachmentIDs) > 0 {
		payload.AttachmentIDs = make([]string, 0, len(attachmentIDs))
		for _, id := range attachmentIDs {
			if id.Valid {
				payload.AttachmentIDs = append(payload.AttachmentIDs, util.UUIDToString(id))
			}
		}
	}
	contextJSON, err := json.Marshal(payload)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("marshal quick-create context: %w", err)
	}

	// The requester who submitted the quick-create modal is the direct_human
	// originator and accountable. Quick-create is the ONE enqueue path with no
	// antecedent row to point the uniform evidence pair at: the run's whole job is
	// to CREATE the issue, so at enqueue time there is no comment / issue / session
	// / run to reference (the issue is linked back later via LinkTaskToIssue).
	// Evidence is therefore intentionally NULL; the accountable human is captured on
	// originator/accountable_user_id, so this is not a NULL-source bypass — source
	// is still stamped direct_human (MUL-4302 §2).
	attr := attribution.DirectHumanRun(requesterID, "", pgtype.UUID{})
	// An unresolved requester degrades to owner_fallback (accountable = agent
	// owner), or is refused if the workspace is fail-closed (MUL-4302 §3.5).
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	attrSource, _, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	runtimeMCPOverlay := s.buildRuntimeMCPOverlay(ctx, requesterID, agent)
	taskID := dbid.NewV7()
	createParams := db.CreateQuickCreateTaskParams{
		ID:                   taskID,
		AgentID:              agentID,
		RuntimeID:            agent.RuntimeID,
		Priority:             priorityToInt("high"),
		Context:              contextJSON,
		OriginatorUserID:     requesterID,
		AccountableUserID:    attr.AccountableUserID,
		RuntimeMcpOverlay:    runtimeMCPOverlay.Overlay,
		RuntimeConnectedApps: runtimeMCPOverlay.ConnectedApps,
		OriginatorSource:     attrSource,
		TriggerEvidenceKind:  attrEvidenceKind,
		TriggerEvidenceRefID: attrEvidenceRef,
	}
	var task db.AgentTaskQueue
	if capture == nil {
		task, err = s.Queries.CreateQuickCreateTask(ctx, createParams)
	} else {
		tx, beginErr := s.TxStarter.Begin(ctx)
		if beginErr != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("begin quick-create source context tx: %w", beginErr)
		}
		defer tx.Rollback(ctx)
		qtx := s.Queries.WithTx(tx)
		if _, err = qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
			ID: capture.SourceIssueID, WorkspaceID: workspaceID,
		}); err == nil {
			var locked []pgtype.UUID
			locked, err = qtx.LockCommentAncestorPath(ctx, db.LockCommentAncestorPathParams{
				CommentID: capture.AnchorCommentID, WorkspaceID: workspaceID, IssueID: capture.SourceIssueID,
			})
			if err == nil && len(locked) == 0 {
				err = ErrAnchorCommentDeleted
			}
		}
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrSourceIssueDeleted
		}
		if err == nil {
			var current SourceContextBuild
			current, err = BuildSourceContext(ctx, qtx, workspaceID, capture.AnchorCommentID)
			if err == nil && current.Digest != capture.Digest {
				err = ErrSourceContextChanged
			}
		}
		if err == nil {
			task, err = qtx.CreateQuickCreateTask(ctx, createParams)
		}
		if err == nil {
			_, err = PersistSourceContext(ctx, qtx, *capture, pgtype.UUID{}, task.ID)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
	}
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("create quick-create task with source context: %w", err)
	}

	slog.Info("quick-create task enqueued",
		"task_id", util.UUIDToString(task.ID),
		"agent_id", util.UUIDToString(agentID),
		"squad_id", payload.SquadID,
		"requester_id", util.UUIDToString(requesterID),
		"workspace_id", util.UUIDToString(workspaceID),
		"project_id", payload.ProjectID,
		"parent_issue_id", payload.ParentIssueID,
	)
	// Match every other Enqueue* path: kick the daemon WS so the task
	// gets claimed promptly instead of waiting for the next 30 s poll
	// cycle. Without this the user perceives "quick create never
	// triggered" because the modal closes immediately and the task
	// sits in 'queued' until the next sleepWithContextOrWakeup tick.
	s.NotifyTaskEnqueued(ctx, task)
	return task, nil
}

// RetrySourceContextQuickCreate manually retries a failed issue-less
// quick-create while retaining the exact pending source context and cloned
// attachments. The context row is the serialization point: only the task that
// currently owns origin_task_id may create and receive a successor, so a
// double-click or a race with automatic retry cannot mint two live attempts.
//
// Only the original requester may retry this recovery item. That keeps the
// direct_human attribution truthful and prevents another workspace member from
// replaying a private prompt merely by learning the task ID.
func (s *TaskService) RetrySourceContextQuickCreate(ctx context.Context, workspaceID, requesterID, sourceTaskID pgtype.UUID, canInvoke func(agent db.Agent) bool) (*db.AgentTaskQueue, error) {
	parent, err := s.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{
		ID: sourceTaskID, WorkspaceID: workspaceID,
	})
	if err != nil || parent.Status != "failed" {
		return nil, ErrSourceContextRetryUnavailable
	}
	quickCreate, ok := s.parseQuickCreateContext(parent)
	if !ok || quickCreate.SourceContextID == "" || quickCreate.RequesterID != util.UUIDToString(requesterID) {
		return nil, ErrSourceContextRetryUnavailable
	}
	contextID, err := util.ParseUUID(quickCreate.SourceContextID)
	if err != nil {
		return nil, ErrSourceContextRetryUnavailable
	}
	agent, err := s.Queries.GetAgent(ctx, parent.AgentID)
	if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		return nil, ErrSourceContextRetryUnavailable
	}
	if canInvoke != nil && !canInvoke(agent) {
		return nil, ErrRerunInvokeNotAllowed
	}
	if err := CheckIssueCreateCapacity(ctx, s.Queries, s.Entitlements, workspaceID); err != nil {
		return nil, fmt.Errorf("preflight quick-create issue capacity: %w", err)
	}
	overlay := s.buildRuntimeMCPOverlay(ctx, requesterID, agent)

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin source context manual retry: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	locked, err := qtx.GetPendingIssueSourceContextByOriginTask(ctx, db.GetPendingIssueSourceContextByOriginTaskParams{
		WorkspaceID: workspaceID, OriginTaskID: sourceTaskID,
	})
	if err != nil || locked.ID != contextID {
		return nil, ErrSourceContextRetryUnavailable
	}
	child, err := qtx.CreateManualQuickCreateRetryTask(ctx, db.CreateManualQuickCreateRetryTaskParams{
		ActorUserID:          requesterID,
		RuntimeMcpOverlay:    overlay.Overlay,
		RuntimeConnectedApps: overlay.ConnectedApps,
		NewTaskID:            dbid.NewV7(),
		SourceTaskID:         sourceTaskID,
	})
	if err != nil {
		return nil, fmt.Errorf("create source context manual retry: %w", err)
	}
	if _, err := qtx.TransferPendingIssueSourceContextTask(ctx, db.TransferPendingIssueSourceContextTaskParams{
		NewTaskID: child.ID, WorkspaceID: workspaceID, ID: contextID, OldTaskID: sourceTaskID,
	}); err != nil {
		return nil, fmt.Errorf("transfer source context manual retry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit source context manual retry: %w", err)
	}
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, child)
	s.NotifyTaskEnqueued(ctx, child)
	return &child, nil
}

// ErrChatTaskAgentArchived signals that EnqueueChatTask refused to
// queue work because the destination agent has been archived. This
// is a productizable state — surface it to the user as "this agent
// has been archived" rather than retrying.
var ErrChatTaskAgentArchived = errors.New("chat task: agent archived")

// ErrChatTaskAgentNoRuntime signals that EnqueueChatTask refused to
// queue work because the agent has never been associated with a
// runtime (agent.runtime_id IS NULL). This is the "agent has no
// daemon configured" case — productizable as "agent offline".
//
// IMPORTANT: this is NOT the same as "the daemon is currently
// disconnected". When agent.runtime_id IS set, EnqueueChatTask
// enqueues the task and the daemon claims it on next online; that
// path returns a task row, not this error.
var ErrChatTaskAgentNoRuntime = errors.New("chat task: agent has no runtime")

// ErrChatQuickActionsNoTurn signals that a quick-actions regeneration was asked
// for a session with no eligible assistant turn to resume from (empty session,
// or the latest turn is a no_response / failure with nothing to suggest from).
var ErrChatQuickActionsNoTurn = errors.New("chat quick actions: no assistant turn to regenerate")

// ErrChatQuickActionsUnavailable signals that the deployment has no LLM layer
// configured (no MULTICA_LLM_API_KEY / MULTICA_LLM_BASE_URL), so suggestions
// cannot be generated at all. Automatic generation degrades silently in that
// case; an explicit refresh gets this error so the client can say why nothing
// happened.
var ErrChatQuickActionsUnavailable = errors.New("chat quick actions: llm layer not configured")

// ErrChatQuickActionsStale signals the turn the client asked to refresh is no
// longer the session's latest assistant turn (a newer reply landed since the
// refresh button was rendered). The regeneration is refused so the client's
// optimistic pending marker never points at a turn the resulting
// chat:quick_actions event will not match (MUL-5149).
var ErrChatQuickActionsStale = errors.New("chat quick actions: refresh target is stale")

// ErrChatQuickActionsBusy signals the session already has work in flight — a
// running turn about to change the latest reply, or another regenerate pass —
// so a new refresh is refused to avoid a stale-target supplement or a duplicate
// quota-spending pass (MUL-5149).
var ErrChatQuickActionsBusy = errors.New("chat quick actions: session busy")

// ErrChatSessionArchived signals that a send or a debounced channel flush lost
// a race with archiving the session and therefore must not persist a new turn.
var ErrChatSessionArchived = errors.New("chat task: session archived")

// PreparedChatTaskEnqueue is an opaque, side-effect-free input snapshot built
// before a caller opens a task-enqueue transaction.
type PreparedChatTaskEnqueue struct {
	accountableUser  pgtype.UUID
	attrSource       pgtype.Text
	attrEvidenceKind pgtype.Text
	runtimeOverlay   runtimeMCPOverlayData
}

// PrepareChatTaskEnqueue performs reads and optional external integration work
// before the caller opens a transaction. BuildTaskOverlay may perform network
// I/O and must never run while /new holds route-rotation locks.
func (s *TaskService) PrepareChatTaskEnqueue(
	ctx context.Context,
	agentID, initiatorUserID pgtype.UUID,
) (PreparedChatTaskEnqueue, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		slog.Error("chat task preparation failed", "agent_id", util.UUIDToString(agentID), "error", err)
		return PreparedChatTaskEnqueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return PreparedChatTaskEnqueue{}, ErrChatTaskAgentArchived
	}
	if !agent.RuntimeID.Valid {
		return PreparedChatTaskEnqueue{}, ErrChatTaskAgentNoRuntime
	}

	attr := attribution.DirectHumanRun(
		initiatorUserID, attribution.EvidenceChat, pgtype.UUID{},
	)
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		slog.Warn("chat task enqueue refused: attribution fail-closed",
			"agent_id", util.UUIDToString(agentID))
		return PreparedChatTaskEnqueue{}, err
	}
	attrSource, _, attrEvidenceKind, _ := attributionCreateParams(attr)
	return PreparedChatTaskEnqueue{
		accountableUser: attr.AccountableUserID,
		attrSource:      attrSource, attrEvidenceKind: attrEvidenceKind,
		runtimeOverlay: s.buildRuntimeMCPOverlay(ctx, initiatorUserID, agent),
	}, nil
}

// EnqueueChatTask creates a Direct Chat task. If the Chat also has a channel
// binding, its context generation is still snapshotted, but no external delivery
// snapshot is created: first-party sends reply only to first-party clients.
func (s *TaskService) EnqueueChatTask(
	ctx context.Context,
	chatSession db.ChatSession,
	initiatorUserID pgtype.UUID,
	forceFreshSession bool,
) (db.AgentTaskQueue, error) {
	return s.enqueueChatTask(
		ctx, chatSession, initiatorUserID, forceFreshSession, 0, false,
		pgtype.UUID{}, 0,
	)
}

// EnqueueChannelChatTask creates a channel-owned task for one durable context
// generation and freezes its external delivery route in the same transaction.
func (s *TaskService) EnqueueChannelChatTask(
	ctx context.Context,
	chatSession db.ChatSession,
	initiatorUserID pgtype.UUID,
	forceFreshSession bool,
	contextRevision int64,
	bindingID pgtype.UUID,
	routeRevision int64,
) (db.AgentTaskQueue, error) {
	if contextRevision <= 0 {
		return db.AgentTaskQueue{}, errors.New("channel chat task requires a context revision")
	}
	return s.enqueueChatTask(
		ctx, chatSession, initiatorUserID, forceFreshSession,
		contextRevision, true, bindingID, routeRevision,
	)
}

func (s *TaskService) enqueueChatTask(
	ctx context.Context,
	chatSession db.ChatSession,
	initiatorUserID pgtype.UUID,
	forceFreshSession bool,
	contextRevision int64,
	requireDelivery bool,
	expectedBindingID pgtype.UUID,
	expectedRouteRevision int64,
) (db.AgentTaskQueue, error) {
	prepared, err := s.PrepareChatTaskEnqueue(ctx, chatSession.AgentID, initiatorUserID)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	if s.TxStarter == nil {
		return db.AgentTaskQueue{}, errors.New("chat task enqueue: transaction starter is required")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("begin chat task enqueue: %w", err)
	}
	defer tx.Rollback(ctx)
	task, err := s.enqueueChatTaskTx(
		ctx, s.Queries.WithTx(tx), chatSession, initiatorUserID,
		forceFreshSession, contextRevision, requireDelivery,
		expectedBindingID, expectedRouteRevision, prepared,
	)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("commit chat task enqueue: %w", err)
	}
	s.FinalizeChatTaskEnqueue(ctx, task)
	return task, nil
}

// EnqueuePreparedChannelChatTaskInTx adds the first /new turn to a caller-owned
// route-rotation transaction. Preparation must happen before that transaction;
// post-commit effects belong to FinalizeChatTaskEnqueue.
func (s *TaskService) EnqueuePreparedChannelChatTaskInTx(
	ctx context.Context,
	tx pgx.Tx,
	chatSession db.ChatSession,
	initiatorUserID pgtype.UUID,
	forceFreshSession bool,
	contextRevision int64,
	prepared PreparedChatTaskEnqueue,
) (db.AgentTaskQueue, error) {
	if contextRevision <= 0 {
		return db.AgentTaskQueue{}, errors.New("prepared channel chat task requires a context revision")
	}
	return s.enqueueChatTaskTx(
		ctx, s.Queries.WithTx(tx), chatSession, initiatorUserID,
		forceFreshSession, contextRevision, true, pgtype.UUID{}, 0, prepared,
	)
}

func (s *TaskService) enqueueChatTaskTx(
	ctx context.Context,
	qtx *db.Queries,
	chatSession db.ChatSession,
	initiatorUserID pgtype.UUID,
	forceFreshSession bool,
	contextRevision int64,
	requireDelivery bool,
	expectedBindingID pgtype.UUID,
	expectedRouteRevision int64,
	prepared PreparedChatTaskEnqueue,
) (db.AgentTaskQueue, error) {
	currentSession, err := qtx.LockChatSessionForEnqueue(ctx, chatSession.ID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("lock chat session: %w", err)
	}
	if currentSession.Status != "active" {
		return db.AgentTaskQueue{}, ErrChatSessionArchived
	}

	agent, err := qtx.GetAgentForClaimUpdate(ctx, chatSession.AgentID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("reload chat agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return db.AgentTaskQueue{}, ErrChatTaskAgentArchived
	}
	if !agent.RuntimeID.Valid {
		return db.AgentTaskQueue{}, ErrChatTaskAgentNoRuntime
	}

	binding, bindingErr := qtx.LockChannelChatSessionBindingForContext(ctx, chatSession.ID)
	if bindingErr != nil && !errors.Is(bindingErr, pgx.ErrNoRows) {
		return db.AgentTaskQueue{}, fmt.Errorf("lock channel chat binding: %w", bindingErr)
	}
	if requireDelivery {
		if errors.Is(bindingErr, pgx.ErrNoRows) {
			return db.AgentTaskQueue{}, fmt.Errorf("lock channel chat binding: %w", pgx.ErrNoRows)
		}
		routeMatches := expectedBindingID.Valid &&
			binding.ID == expectedBindingID &&
			binding.RouteRevision == expectedRouteRevision
		if binding.RetiredAt.Valid && !routeMatches {
			return db.AgentTaskQueue{}, fmt.Errorf("lock retired channel chat binding: %w", pgx.ErrNoRows)
		}
		if expectedBindingID.Valid && !routeMatches {
			return db.AgentTaskQueue{}, fmt.Errorf("lock expected channel chat route: %w", pgx.ErrNoRows)
		}
	}

	pendingFresh := false
	if bindingErr == nil {
		if contextRevision <= 0 {
			contextRevision = binding.ContextRevision
		}
		generation, err := qtx.LockChannelChatContextGenerationByRevision(
			ctx, db.LockChannelChatContextGenerationByRevisionParams{
				ChatSessionID: chatSession.ID, Revision: contextRevision,
			},
		)
		if err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("lock channel context generation: %w", err)
		}
		pendingFresh = generation.PendingFresh
	}
	if pendingFresh {
		forceFreshSession = true
	}

	mediaPendingUntil, err := qtx.GetChannelMediaPendingUntil(
		ctx, db.GetChannelMediaPendingUntilParams{
			ChatSessionID: chatSession.ID,
			ChannelContextRevision: pgtype.Int8{
				Int64: contextRevision, Valid: contextRevision > 0,
			},
		},
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.AgentTaskQueue{}, fmt.Errorf("load channel media pending deadline: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		mediaPendingUntil = pgtype.Timestamptz{}
	}

	task, err := qtx.CreateChatTask(ctx, db.CreateChatTaskParams{
		ID:                   dbid.NewV7(),
		AgentID:              chatSession.AgentID,
		RuntimeID:            agent.RuntimeID,
		Priority:             2,
		ChatSessionID:        chatSession.ID,
		InitiatorUserID:      initiatorUserID,
		FireAt:               mediaPendingUntil,
		OriginatorUserID:     initiatorUserID,
		AccountableUserID:    prepared.accountableUser,
		ForceFreshSession:    pgtype.Bool{Bool: forceFreshSession, Valid: true},
		RuntimeMcpOverlay:    prepared.runtimeOverlay.Overlay,
		RuntimeConnectedApps: prepared.runtimeOverlay.ConnectedApps,
		OriginatorSource:     prepared.attrSource,
		TriggerEvidenceKind:  prepared.attrEvidenceKind,
		TriggerEvidenceRefID: chatSession.ID,
		ChannelContextRevision: pgtype.Int8{
			Int64: contextRevision, Valid: contextRevision > 0,
		},
	})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("create chat task: %w", err)
	}
	if requireDelivery {
		if _, err := qtx.CreateChannelTaskDeliveryFromSession(
			ctx, db.CreateChannelTaskDeliveryFromSessionParams{
				TaskID: task.ID, ChatSessionID: chatSession.ID,
			},
		); err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("snapshot channel task delivery: %w", err)
		}
	}

	task, err = qtx.SetChatTaskInputOwnerSelf(ctx, task.ID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("set channel chat task input owner: %w", err)
	}
	if err := qtx.LinkUnownedChannelChatMessagesToTask(
		ctx, db.LinkUnownedChannelChatMessagesToTaskParams{
			TaskID: task.ID, ChatSessionID: chatSession.ID,
		},
	); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("seal channel chat task input: %w", err)
	}
	switch corrected, err := qtx.DeferChatTaskForSealedPendingMedia(ctx, task.ID); {
	case err == nil:
		task = corrected
	case !errors.Is(err, pgx.ErrNoRows):
		return db.AgentTaskQueue{}, fmt.Errorf("defer chat task for sealed pending media: %w", err)
	}
	if pendingFresh {
		if err := qtx.ClearChannelChatContextPendingFresh(
			ctx, db.ClearChannelChatContextPendingFreshParams{
				ChatSessionID: chatSession.ID, Revision: contextRevision,
			},
		); err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("clear channel context pending fresh: %w", err)
		}
		if err := qtx.ClearChannelChatSessionPendingFreshForRevision(
			ctx, db.ClearChannelChatSessionPendingFreshForRevisionParams{
				ChatSessionID: chatSession.ID, Revision: contextRevision,
			},
		); err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("clear channel pending fresh: %w", err)
		}
	}
	return task, nil
}

// FinalizeChatTaskEnqueue performs only post-commit effects.
func (s *TaskService) FinalizeChatTaskEnqueue(ctx context.Context, task db.AgentTaskQueue) {
	if task.Status == "deferred" {
		slog.Info("chat task deferred for channel media",
			"task_id", util.UUIDToString(task.ID),
			"chat_session_id", util.UUIDToString(task.ChatSessionID),
			"agent_id", util.UUIDToString(task.AgentID),
			"fire_at", task.FireAt.Time,
		)
		if err := s.PromoteChannelChatTasksIfMediaReady(ctx, task.ChatSessionID); err != nil {
			slog.Warn("chat task media-ready fence failed; deferred task falls back to its deadline",
				"task_id", util.UUIDToString(task.ID),
				"chat_session_id", util.UUIDToString(task.ChatSessionID),
				"error", err,
			)
		}
		return
	}

	slog.Info("chat task enqueued",
		"task_id", util.UUIDToString(task.ID),
		"chat_session_id", util.UUIDToString(task.ChatSessionID),
		"agent_id", util.UUIDToString(task.AgentID),
	)
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.NotifyTaskEnqueued(ctx, task)
}

// RegenerateChatQuickActions runs a fresh suggestion pass for the session's
// latest assistant turn, letting the user refresh the quick-action pills
// without sending a new message (MUL-5149). Generation happens server-side
// through the same LLM path as the automatic pass, so this enqueues no agent
// task and resumes no provider session — it just validates the target. It
// returns the target assistant message id (so the client can anchor its pending
// placeholder there) and the turn's task row, which the caller hands to
// GenerateChatQuickActionsAsync.
//
// This is an explicit user action, so it ignores the per-device quick-actions
// toggle (which only gates automatic generation at send time).
func (s *TaskService) RegenerateChatQuickActions(ctx context.Context, chatSession db.ChatSession, expectedMessageID pgtype.UUID) (targetMessageID pgtype.UUID, targetTask db.AgentTaskQueue, err error) {
	if s.QuickActions == nil || !s.QuickActions.Enabled() {
		return pgtype.UUID{}, db.AgentTaskQueue{}, ErrChatQuickActionsUnavailable
	}

	// The target is the latest assistant turn. Only an ordinary message turn
	// can seed suggestions — a no_response / failure turn has nothing to build
	// on, and the generator keys its write off the turn's task id.
	target, err := s.Queries.GetLatestAssistantChatMessageForSession(ctx, chatSession.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, db.AgentTaskQueue{}, ErrChatQuickActionsNoTurn
		}
		return pgtype.UUID{}, db.AgentTaskQueue{}, fmt.Errorf("load latest assistant turn: %w", err)
	}
	if target.MessageKind != protocol.ChatMessageKindMessage || !target.TaskID.Valid {
		return pgtype.UUID{}, db.AgentTaskQueue{}, ErrChatQuickActionsNoTurn
	}
	// Confirm the client is refreshing the turn that is STILL the latest. Under
	// a multi-client race a newer reply can land between the button rendering
	// and this request; regenerating then would build suggestions from the
	// newer context yet attach them to the stale turn, and the client's pending
	// marker would wait on a chat:quick_actions that never matches. Refuse so
	// the client rolls back and re-offers refresh on the new turn (MUL-5149).
	if target.ID != expectedMessageID {
		return pgtype.UUID{}, db.AgentTaskQueue{}, ErrChatQuickActionsStale
	}
	// Refuse while a turn is already running on this session: it is about to
	// replace the latest reply, so suggestions built now would be attached to a
	// turn the user is seconds away from scrolling past.
	busy, err := s.Queries.HasActiveChatTaskForSession(ctx, chatSession.ID)
	if err != nil {
		return pgtype.UUID{}, db.AgentTaskQueue{}, fmt.Errorf("check active chat task: %w", err)
	}
	if busy {
		return pgtype.UUID{}, db.AgentTaskQueue{}, ErrChatQuickActionsBusy
	}
	// A refresh no longer creates a task row, so the check above cannot see a
	// generation already running for this session — a second refresh (or one
	// racing the automatic pass) would otherwise be accepted, spend a second
	// upstream call, and race the first to write the same row. Refuse it here
	// so the client rolls its optimistic marker back instead.
	if s.chatQuickActionsInFlight(chatSession.ID) {
		return pgtype.UUID{}, db.AgentTaskQueue{}, ErrChatQuickActionsBusy
	}

	task, err := s.Queries.GetAgentTask(ctx, target.TaskID)
	if err != nil {
		return pgtype.UUID{}, db.AgentTaskQueue{}, fmt.Errorf("load target turn task: %w", err)
	}

	slog.Info("chat quick-actions regenerate accepted",
		"chat_session_id", util.UUIDToString(chatSession.ID),
		"target_task_id", util.UUIDToString(target.TaskID),
		"target_message_id", util.UUIDToString(target.ID),
	)
	// Validation only: the caller starts the pass. Keeping dispatch out of here
	// means the refresh gates can be tested without a background goroutine
	// writing to the same rows mid-assertion.
	return target.ID, task, nil
}

// PromoteChannelChatTasksIfMediaReady queues channel tasks as soon as every
// unexpired media marker in the session has been cleared. If the process dies
// first, the normal deferred-task promoter queues them at their persisted
// fire_at deadline, preserving the placeholder fallback across restarts.
func (s *TaskService) PromoteChannelChatTasksIfMediaReady(ctx context.Context, sessionID pgtype.UUID) error {
	tasks, err := s.Queries.PromoteChannelChatTasksIfMediaReady(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("promote channel chat tasks after media: %w", err)
	}
	for _, task := range tasks {
		slog.Info("channel media-ready chat task promoted",
			"task_id", util.UUIDToString(task.ID),
			"chat_session_id", util.UUIDToString(sessionID),
			"agent_id", util.UUIDToString(task.AgentID),
		)
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
		s.NotifyTaskEnqueued(ctx, task)
	}
	return nil
}

// PromoteDeferredChannelIssueTask makes a media-gated /issue task claimable.
// ErrNoRows means the deadline sweeper already promoted it (or the task was
// cancelled), so this is idempotent across the two promotion paths.
func (s *TaskService) PromoteDeferredChannelIssueTask(ctx context.Context, taskID pgtype.UUID) error {
	task, err := s.Queries.PromoteDeferredChannelIssueTask(ctx, taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("promote deferred channel issue task: %w", err)
	}
	slog.Info("channel media-ready issue task promoted",
		"task_id", util.UUIDToString(task.ID),
		"issue_id", util.UUIDToString(task.IssueID),
		"agent_id", util.UUIDToString(task.AgentID),
	)
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.NotifyTaskEnqueued(ctx, task)
	return nil
}

// DirectChatSendResult carries the rows a transactional direct-chat send
// persisted, so the handler can broadcast the user message and shape its
// response without re-reading them.
type DirectChatSendResult struct {
	Task               db.AgentTaskQueue
	Message            db.ChatMessage
	BoundAttachmentIDs []pgtype.UUID
	Queued             bool
	InitialTitle       string
}

var ErrChatSessionAlreadyStarted = errors.New("chat session already has a user message")

// SendDirectChatMessage atomically persists one web/mobile direct-chat turn:
// the owning task (which claims its own input batch via chat_input_task_id), the
// user message bound to that task, any attachment bindings, and the session
// touch all commit together (MUL-4351). The daemon is only notified after the
// commit, so it can never observe a message without a task or a task without its
// input owner — and a later claim reads exactly this task's user messages
// instead of scanning trailing history.
//
// The caller must have already gated the session and preflighted the agent
// (archived / no-runtime), passing the loaded agent in. Those checks are repeated
// under the transaction locks below because either row may change before enqueue.
func (s *TaskService) SendDirectChatMessage(
	ctx context.Context,
	session db.ChatSession,
	agent db.Agent,
	initiatorUserID pgtype.UUID,
	content string,
	attachmentIDs []pgtype.UUID,
	uploaderType string,
	uploaderID pgtype.UUID,
) (*DirectChatSendResult, error) {
	// Build the per-task Composio overlay before the transaction — it can do
	// network I/O and must not run with a DB transaction open.
	overlay := s.buildRuntimeMCPOverlay(ctx, initiatorUserID, agent)

	// Full attribution for the chat sender, resolved before the tx (the policy read
	// + fallback must not run with a transaction open) — the same direct_human stamp
	// EnqueueChatTask writes. Without this the direct-chat path was a bypass: it set
	// originator_user_id but left accountable_user_id / source / evidence NULL,
	// violating the one-way invariant and dropping the audit source (MUL-4302 §2).
	attr := attribution.DirectHumanRun(initiatorUserID, attribution.EvidenceChat, session.ID)
	attr, err := s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		return nil, err
	}
	attrSource, _, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)

	var out DirectChatSendResult
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		// Serialise this send against a concurrent runtime rebind of the same
		// session (MUL-5163). The lock must be taken first and the agent re-read
		// under it: the runtime_id the caller loaded can already be stale by the
		// time we get here, and a send blocked behind a rebind would otherwise
		// resume and stamp its task with the runtime the switch just moved away
		// from — leaving the user with a "switched" confirmation and a reply
		// running on the old runtime. Locking chat_session first also matches the
		// delete path's lock order, so the two cannot deadlock.
		if _, err := qtx.LockChatSessionForRuntimeBind(ctx, session.ID); err != nil {
			return fmt.Errorf("lock chat session: %w", err)
		}
		currentSession, err := qtx.GetChatSession(ctx, session.ID)
		if err != nil {
			return fmt.Errorf("reload chat session: %w", err)
		}
		if currentSession.Status != "active" {
			return ErrChatSessionArchived
		}
		carrier, err := qtx.GetAgentForClaimUpdate(ctx, session.AgentID)
		if err != nil {
			return fmt.Errorf("reload chat agent: %w", err)
		}
		if carrier.ArchivedAt.Valid {
			return ErrChatTaskAgentArchived
		}
		if !carrier.RuntimeID.Valid {
			return ErrChatTaskAgentNoRuntime
		}

		// The database status of every newly-created task is "queued" until a
		// daemon claims it. Product queue semantics are positional instead: this
		// send is a follow-up only when another visible task in the same session
		// is already ahead of it. Deferred retries count because they resume an
		// older turn before this one. The session + agent locks serialize sibling
		// sends, retries, terminal writes, and claims around this read.
		queued, err := qtx.HasPendingChatTurnForSession(ctx, session.ID)
		if err != nil {
			return fmt.Errorf("check direct chat queue position: %w", err)
		}
		out.Queued = queued

		task, err := qtx.CreateChatTask(ctx, db.CreateChatTaskParams{
			ID:                   dbid.NewV7(),
			AgentID:              session.AgentID,
			RuntimeID:            carrier.RuntimeID,
			Priority:             2, // medium priority for chat; matches EnqueueChatTask
			ChatSessionID:        session.ID,
			InitiatorUserID:      initiatorUserID,
			OriginatorUserID:     attr.UserID,
			AccountableUserID:    attr.AccountableUserID,
			ForceFreshSession:    pgtype.Bool{Bool: false, Valid: true},
			RuntimeMcpOverlay:    overlay.Overlay,
			RuntimeConnectedApps: overlay.ConnectedApps,
			OriginatorSource:     attrSource,
			TriggerEvidenceKind:  attrEvidenceKind,
			TriggerEvidenceRefID: attrEvidenceRef,
		})
		if err != nil {
			return fmt.Errorf("create direct chat task: %w", err)
		}
		// Claim this task's own input batch (chat_input_task_id = id) in the same
		// transaction, before the user message is written with task_id = task.id.
		task, err = qtx.SetChatTaskInputOwnerSelf(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("stamp direct chat input owner: %w", err)
		}
		out.Task = task

		// Adopt the onboarding kickoff, if this session still has an unowned one.
		// It is written by OpenMikaOnboardingChat with no task, so this is the
		// only thing that ever delivers it to a runtime — and it must happen
		// before the member's own row is written, so the batch reads as
		// "context, then their message" once ordered by created_at.
		//
		// A no-op for every other session: only Mika onboarding writes that kind,
		// and only the first send of one finds it unowned.
		if err := qtx.AdoptOrphanOnboardingKickoff(ctx, db.AdoptOrphanOnboardingKickoffParams{
			ChatSessionID: session.ID,
			TaskID:        task.ID,
		}); err != nil {
			return fmt.Errorf("adopt onboarding kickoff: %w", err)
		}

		// Initialize an explicitly empty Chat before inserting its first public
		// user message. The CAS query also protects a manual rename and competing
		// first send; keeping it in this transaction prevents a title-only commit.
		if title := chattitle.Derive(content); title != "" {
			if _, err := qtx.InitializeChatSessionTitle(ctx, db.InitializeChatSessionTitleParams{ID: session.ID, Title: title}); err == nil {
				out.InitialTitle = title
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("initialize direct chat title: %w", err)
			}
		}

		// Create the user message already owned by this task (task_id = task.id),
		// so it belongs to this immutable input batch the instant it exists.
		msg, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ID:            dbid.NewV7(),
			ChatSessionID: session.ID,
			Role:          "user",
			Content:       content,
			TaskID:        task.ID,
			MessageKind:   pgtype.Text{String: protocol.ChatMessageKindMessage, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("create user chat message: %w", err)
		}
		out.Message = msg

		if len(attachmentIDs) > 0 {
			bound, err := qtx.LinkAttachmentsToChatMessage(ctx, db.LinkAttachmentsToChatMessageParams{
				ChatMessageID: msg.ID,
				ChatSessionID: session.ID,
				WorkspaceID:   session.WorkspaceID,
				UploaderType:  uploaderType,
				UploaderID:    uploaderID,
				AttachmentIds: attachmentIDs,
			})
			if err != nil {
				return fmt.Errorf("link chat attachments: %w", err)
			}
			out.BoundAttachmentIDs = bound

			// An explicitly empty /new Chat can receive its first turn from a
			// first-party client. For an attachment-only turn, initialize the
			// title from the first attachment that was actually bound. The media
			// title CAS runs after the message insert by design and still refuses
			// to overwrite a manual rename or a competing first message.
			if out.InitialTitle == "" && strings.TrimSpace(content) == "" && len(bound) > 0 {
				attachments, err := qtx.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{
					ChatMessageID: msg.ID,
					WorkspaceID:   session.WorkspaceID,
				})
				if err != nil {
					return fmt.Errorf("list bound chat attachments for title: %w", err)
				}
				if len(attachments) > 0 {
					title := chattitle.Derive(attachments[0].Filename)
					if title != "" {
						if _, err := qtx.InitializeChatSessionMediaTitle(ctx, db.InitializeChatSessionMediaTitleParams{
							ID: session.ID, MessageID: msg.ID, Title: title,
						}); err == nil {
							out.InitialTitle = title
						} else if !errors.Is(err, pgx.ErrNoRows) {
							return fmt.Errorf("initialize direct attachment chat title: %w", err)
						}
					}
				}
			}
		}

		if err := qtx.TouchChatSession(ctx, session.ID); err != nil {
			return fmt.Errorf("touch chat session: %w", err)
		}
		return nil
	}); err != nil {
		slog.Error("direct chat send failed",
			"chat_session_id", util.UUIDToString(session.ID),
			"agent_id", util.UUIDToString(session.AgentID),
			"error", err)
		return nil, err
	}

	slog.Info("direct chat task enqueued",
		"task_id", util.UUIDToString(out.Task.ID),
		"chat_session_id", util.UUIDToString(session.ID),
		"agent_id", util.UUIDToString(session.AgentID))
	// Notify only after commit. See EnqueueTaskForIssue for ordering rationale.
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, out.Task)
	s.NotifyTaskEnqueued(ctx, out.Task)
	return &out, nil
}

// MikaOnboardingOpenResult carries the two rows that open a Mika conversation.
type MikaOnboardingOpenResult struct {
	// Kickoff is the hidden product context. It is written WITHOUT a task —
	// the member's first real send adopts it (AdoptOrphanOnboardingKickoff).
	Kickoff db.ChatMessage
	// Opening is what the member reads, already final. No agent produced it,
	// so it carries no task id, no elapsed time, and nothing to regenerate.
	Opening db.ChatMessage
}

// OpenMikaOnboardingChat writes a Mika conversation's first two rows in one
// transaction: the hidden kickoff and the product-authored opening the member
// sees (MUL-5827). Nothing is enqueued — this used to be a full chat task, and
// the member waited out a runtime cold start to read a reply the product had
// already decided word for word.
//
// "Session is still empty" is enforced under the chat-session lock, so retries,
// React double-submits, and two clients racing the same session still produce
// at most one opening. The kickoff row is what makes that check work: it is a
// role='user' row, so ChatSessionHasUserMessage sees it exactly as it saw the
// old kickoff turn.
func (s *TaskService) OpenMikaOnboardingChat(ctx context.Context, session db.ChatSession, kickoff, opening string) (*MikaOnboardingOpenResult, error) {
	var out MikaOnboardingOpenResult
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		// Same lock and lock ORDER as the send path, so an opening racing a
		// first send or a runtime rebind serializes instead of deadlocking.
		if _, err := qtx.LockChatSessionForRuntimeBind(ctx, session.ID); err != nil {
			return fmt.Errorf("lock chat session: %w", err)
		}
		current, err := qtx.GetChatSession(ctx, session.ID)
		if err != nil {
			return fmt.Errorf("reload chat session: %w", err)
		}
		if current.Status != "active" {
			return ErrChatSessionArchived
		}
		hasUserMessage, err := qtx.ChatSessionHasUserMessage(ctx, session.ID)
		if err != nil {
			return fmt.Errorf("check chat session input: %w", err)
		}
		if hasUserMessage {
			return ErrChatSessionAlreadyStarted
		}

		kickoffRow, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ID:            dbid.NewV7(),
			ChatSessionID: session.ID,
			Role:          "user",
			Content:       kickoff,
			MessageKind:   pgtype.Text{String: protocol.ChatMessageKindOnboardingKickoff, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("create onboarding kickoff: %w", err)
		}
		out.Kickoff = kickoffRow

		// Ordered one microsecond after the kickoff — see the query comment for
		// why a shared transaction timestamp is not good enough here.
		openingRow, err := qtx.CreateMikaOnboardingOpening(ctx, db.CreateMikaOnboardingOpeningParams{
			ID:               dbid.NewV7(),
			ChatSessionID:    session.ID,
			KickoffCreatedAt: kickoffRow.CreatedAt,
			Content:          opening,
		})
		if err != nil {
			return fmt.Errorf("create onboarding opening: %w", err)
		}
		out.Opening = openingRow

		if err := qtx.TouchChatSession(ctx, session.ID); err != nil {
			return fmt.Errorf("touch chat session: %w", err)
		}
		return nil
	}); err != nil {
		if !errors.Is(err, ErrChatSessionAlreadyStarted) {
			slog.Error("mika onboarding open failed",
				"chat_session_id", util.UUIDToString(session.ID),
				"agent_id", util.UUIDToString(session.AgentID),
				"error", err)
		}
		return nil, err
	}
	return &out, nil
}

// CancelTasksForIssue cancels every active task on the issue, reconciles each
// affected agent's status, and broadcasts task:cancelled events so frontends
// clear their live cards.
//
// Callers are explicit issue-lifecycle cleanup paths only — DeleteIssue and
// BatchDeleteIssues, where the owning issue row is going away so its tasks
// must not be left orphaned. A plain status flip, `cancelled` included, no
// longer routes here (MUL-4465): cancelling an issue is not an implicit "stop
// all runs" switch. Do not re-add a status-driven caller.
//
// Before #1587 this path was "cancel rows and return", which left each affected
// agent stuck at status="working" indefinitely, requiring a manual
// `multica agent update <id> --status idle` to unwedge. It now reconciles agent
// status and broadcasts task:cancelled, matching CancelTask and RerunIssue.
func (s *TaskService) CancelTasksForIssue(ctx context.Context, issueID pgtype.UUID) error {
	cancelled, err := s.Queries.CancelAgentTasksByIssue(ctx, issueID)
	if err != nil {
		return err
	}
	for _, t := range cancelled {
		s.captureTaskCancelled(ctx, t)
		s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, t)
	}
	// Reconcile once per distinct agent instead of once per cancelled row:
	// cancelling an issue often stops several tasks owned by the same agent,
	// and each reconcile is a DB write plus a status broadcast. Matches
	// CancelTasksForAgent's single-reconcile shape (D#3319).
	for _, agentID := range distinctAgentIDs(cancelled) {
		s.ReconcileAgentStatus(ctx, agentID)
	}
	s.notifyTasksFinished(cancelled)
	return nil
}

// distinctAgentIDs returns each agent id appearing in the cancelled rows once,
// preserving first-seen order. Bulk cancellations frequently stop several tasks
// owned by the same agent; reconciling per distinct agent (rather than per row)
// collapses the redundant RefreshAgentStatusFromTasks writes and status
// broadcasts down to one per agent without changing the final agent status.
func distinctAgentIDs(cancelled []db.AgentTaskQueue) []pgtype.UUID {
	seen := make(map[pgtype.UUID]struct{}, len(cancelled))
	ids := make([]pgtype.UUID, 0, len(cancelled))
	for _, t := range cancelled {
		if _, dup := seen[t.AgentID]; dup {
			continue
		}
		seen[t.AgentID] = struct{}{}
		ids = append(ids, t.AgentID)
	}
	return ids
}

// CancelTasksForAgent cancels every active task belonging to an agent
// (queued + dispatched + running), reconciles the agent's status, and
// broadcasts task:cancelled events. Used by the agent-level "Cancel all
// tasks" action — same shape as CancelTasksForIssue but scoped on agent_id.
//
// Returns the cancelled rows so callers can report counts / log them.
func (s *TaskService) CancelTasksForAgent(ctx context.Context, agentID pgtype.UUID) ([]db.AgentTaskQueue, error) {
	cancelled, err := s.Queries.CancelAgentTasksByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for _, t := range cancelled {
		s.captureTaskCancelled(ctx, t)
		s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, t)
	}
	// Reconcile once after the loop — agent transitions from
	// working→available based on remaining task counts, no need to call
	// per row (the rows we just cancelled all belong to the same agent).
	s.ReconcileAgentStatus(ctx, agentID)
	s.notifyTasksFinished(cancelled)
	return cancelled, nil
}

// CancelTasksByTriggerComment cancels active tasks whose planned comment batch
// contains the given edited/deleted comment. The historical method name is
// retained for call-site stability. It must run before deletion clears the
// trigger FK; the returned rows let the handler re-route every surviving input.
func (s *TaskService) CancelTasksByTriggerComment(ctx context.Context, commentID pgtype.UUID) ([]db.AgentTaskQueue, error) {
	cancelled, err := s.Queries.CancelAgentTasksByTriggerComment(ctx, commentID)
	if err != nil {
		return nil, err
	}
	for _, t := range cancelled {
		s.captureTaskCancelled(ctx, t)
		s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, t)
	}
	// Reconcile once per distinct agent instead of once per cancelled row: an
	// edited/deleted trigger comment can cancel several tasks owned by the same
	// agent, and each reconcile is a DB write plus a status broadcast (D#3319).
	for _, agentID := range distinctAgentIDs(cancelled) {
		s.ReconcileAgentStatus(ctx, agentID)
	}
	s.notifyTasksFinished(cancelled)
	return cancelled, nil
}

// BroadcastCancelledTasks reconciles each affected agent's status and emits
// task:cancelled for every row. Callers must invoke this AFTER committing the
// cancellation so subscribers don't observe a "cancelled" event for a row
// that the tx might still roll back.
//
// workspaceID comes from the caller instead of being resolved per task, because
// the transaction these callers have just committed can delete the row the
// resolution would read. A chat task's workspace is reached through its
// chat_session, and both DeleteChatSession and the runtime teardown remove that
// session — the teardown by deleting the system agent it hangs off. Afterwards
// ResolveTaskWorkspaceID finds nothing and returns "", and publishTaskEvent
// drops an event with no workspace before it reaches the bus: the rows are
// cancelled, nobody is told, and every queue view and channel indicator keeps
// showing a run that no longer exists. Each caller already knows the workspace
// — it is the one whose session, member or runtime is being torn down — so the
// lookup is not needed and cannot fail.
func (s *TaskService) BroadcastCancelledTasks(ctx context.Context, workspaceID string, cancelled []db.AgentTaskQueue) {
	for _, t := range cancelled {
		s.captureTaskCancelled(ctx, t)
		s.ReconcileAgentStatus(ctx, t.AgentID)
		s.publishTaskEvent(protocol.EventTaskCancelled, workspaceID, t)
	}
	s.notifyTasksFinished(cancelled)
}

// BroadcastTaskQueued emits a post-commit queue invalidation for clients.
func (s *TaskService) BroadcastTaskQueued(ctx context.Context, task db.AgentTaskQueue) {
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
}

func (s *TaskService) CaptureCancelledTasks(ctx context.Context, cancelled []db.AgentTaskQueue) {
	for _, t := range cancelled {
		s.captureTaskCancelled(ctx, t)
	}
}

type CancelledChatMessageResult struct {
	ChatSessionID  string
	MessageID      string
	Content        string
	RestoreToInput bool
	// Attachments are the rows detached from the deleted user message so they
	// survive the ON DELETE CASCADE and can re-bind when the restored draft is
	// re-sent.
	Attachments []db.Attachment
}

type CancelTaskResult struct {
	Task                 db.AgentTaskQueue
	CancelledChatMessage *CancelledChatMessageResult
}

var ErrTaskNoLongerQueued = errors.New("task is no longer queued")

// CancelTaskOptions carries what the caller knows about the client that asked
// for the cancellation.
type CancelTaskOptions struct {
	// ClientSupportsDraftRestore is true when the caller can recover a prompt
	// through the durable draft-restore path (#5219). Only such a client may be
	// handed a deferred outcome; for anyone else the empty-transcript judgment
	// stays synchronous, because the cancel response is their only chance to get
	// the prompt back. See protocol.AppCapabilityChatDraftRestoreV1.
	ClientSupportsDraftRestore bool
	// QueuedOnly turns queue edit/remove into a session-scoped compare-and-set.
	QueuedOnly          bool
	ExpectedChatSession pgtype.UUID
	QueueAction         string
	// ErrorMessage / FailureReason, when set, are persisted onto the cancelled
	// row. Only for cancellations the USER did not ask for (e.g. the worktree
	// claim gate refusing a too-old daemon): a user-initiated cancel needs no
	// explanation, but a server-initiated one without a persisted reason
	// surfaces as an unexplained "cancelled" whose only trace is a 4xx in a
	// daemon log the user never sees.
	ErrorMessage  string
	FailureReason string
	// UserInitiated distinguishes the issue UI/API cancel action from automatic
	// server repairs. An explicit user cancellation terminally acknowledges any
	// delegated-failure recovery signal planned into the task; automatic
	// cancellations must leave that signal replayable.
	UserInitiated bool
}

// CancelTask cancels a single task by ID for an automatic server path. It does
// not acknowledge delegated-failure recovery inputs; use CancelTaskByUser for
// an explicit issue-task cancellation. It broadcasts a task:cancelled event so
// frontends can update immediately.
func (s *TaskService) CancelTask(ctx context.Context, taskID pgtype.UUID) (*db.AgentTaskQueue, error) {
	// Callers of this wrapper are automatic non-chat repair/daemon paths, so
	// finalizeCancelledChatMessage returns before the gate is even read.
	// Should a chat task ever reach here, there is no client waiting on a
	// synchronous restore anyway, and the durable path is the only one that can
	// hand the prompt back at all.
	result, err := s.CancelTaskWithResult(ctx, taskID, CancelTaskOptions{ClientSupportsDraftRestore: true})
	if err != nil {
		return nil, err
	}
	return &result.Task, nil
}

// CancelTaskByUser is the explicit issue-task cancellation path. Unlike an
// automatic server cancellation, it terminally acknowledges any delegated-
// failure recovery signal carried by the task so the sweeper respects the
// user's decision instead of recreating the task.
func (s *TaskService) CancelTaskByUser(ctx context.Context, taskID pgtype.UUID) (*db.AgentTaskQueue, error) {
	result, err := s.CancelTaskWithResult(ctx, taskID, CancelTaskOptions{
		ClientSupportsDraftRestore: true,
		UserInitiated:              true,
	})
	if err != nil {
		return nil, err
	}
	return &result.Task, nil
}

// CancelTaskWithReason cancels a task the SERVER decided to stop, persisting an
// actionable reason onto the row. It runs the same full cancellation flow as
// CancelTask — audit capture, chat settle, agent status reconcile,
// task:cancelled broadcast, NotifyTaskFinished — because a raw
// CancelAgentTaskWithReason query bypass leaves the agent pill running, the
// live card stale, and any capacity/serial waiter unwoken.
func (s *TaskService) CancelTaskWithReason(ctx context.Context, taskID pgtype.UUID, errorMessage, failureReason string) (*db.AgentTaskQueue, error) {
	result, err := s.CancelTaskWithResult(ctx, taskID, CancelTaskOptions{
		ClientSupportsDraftRestore: true,
		ErrorMessage:               errorMessage,
		FailureReason:              failureReason,
	})
	if err != nil {
		return nil, err
	}
	return &result.Task, nil
}

// CancelTaskWithResult cancels a single task and returns any chat-specific
// cleanup result needed by user-facing callers.
func (s *TaskService) CancelTaskWithResult(ctx context.Context, taskID pgtype.UUID, opts CancelTaskOptions) (*CancelTaskResult, error) {
	// Both fields are persisted onto the cancelled row's TEXT columns below, and
	// at least one caller interpolates an externally-supplied path into
	// ErrorMessage. A NUL in either rolls the cancellation back and leaves the
	// task running — the same wedge as GH #7098 on the fail/complete paths.
	opts.ErrorMessage = util.SanitizeTextForPostgres(opts.ErrorMessage)
	opts.FailureReason = util.SanitizeTextForPostgres(opts.FailureReason)

	if opts.UserInitiated && (opts.ErrorMessage != "" || opts.FailureReason != "") {
		return nil, errors.New("user-initiated cancellation cannot carry a server failure reason")
	}
	var (
		task                 db.AgentTaskQueue
		cancelledChatMessage *CancelledChatMessageResult
		err                  error
	)
	if opts.QueuedOnly {
		if opts.QueueAction != "edit" && opts.QueueAction != "remove" {
			return nil, errors.New("queue action must be edit or remove")
		}
		err = s.runInTx(ctx, func(qtx *db.Queries) error {
			if _, err := qtx.LockChatSessionForTask(ctx, taskID); err != nil {
				return fmt.Errorf("lock queued chat session: %w", err)
			}
			task, err = qtx.CancelQueuedAgentTask(ctx, db.CancelQueuedAgentTaskParams{
				ID:            taskID,
				ChatSessionID: opts.ExpectedChatSession,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrTaskNoLongerQueued
			}
			if err != nil {
				return fmt.Errorf("cancel queued task: %w", err)
			}
			cancelledChatMessage, err = s.settleQueuedChatInput(ctx, qtx, task, opts.QueueAction)
			return err
		})
	} else {
		// The status flip and the chat resume-pointer advance commit together. Split
		// across two statements, the `cancelled` status becomes visible to every
		// other connection while the pointer still names the previous turn's
		// session, and a queued follow-up can resume that older session.
		err = s.runInTx(ctx, func(qtx *db.Queries) error {
			if err := lockChatSessionForTaskWrite(ctx, qtx, taskID); err != nil {
				return err
			}
			var (
				cancelled db.AgentTaskQueue
				err       error
			)
			if opts.UserInitiated {
				cancelled, err = qtx.CancelAgentTaskByUser(ctx, taskID)
			} else if opts.ErrorMessage != "" || opts.FailureReason != "" {
				cancelled, err = qtx.CancelAgentTaskWithReason(ctx, db.CancelAgentTaskWithReasonParams{
					ID:            taskID,
					Error:         pgtype.Text{String: opts.ErrorMessage, Valid: opts.ErrorMessage != ""},
					FailureReason: pgtype.Text{String: opts.FailureReason, Valid: opts.FailureReason != ""},
				})
			} else {
				cancelled, err = qtx.CancelAgentTask(ctx, taskID)
			}
			if err != nil {
				return err
			}
			task = cancelled
			if !cancelled.ChatSessionID.Valid {
				return nil
			}
			return qtx.AdvanceCancelledChatSessionPointer(ctx, cancelled.ID)
		})
	}
	if errors.Is(err, ErrTaskNoLongerQueued) {
		return nil, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := s.Queries.GetAgentTask(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("cancel task: %w", err)
		}
		return &CancelTaskResult{Task: existing}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cancel task: %w", err)
	}

	slog.Info("task cancelled", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(task.IssueID))
	s.captureTaskCancelled(ctx, task)
	if !opts.QueuedOnly {
		cancelledChatMessage = s.finalizeCancelledChatMessage(ctx, task, opts)
	}

	// Reconcile agent status
	s.ReconcileAgentStatus(ctx, task.AgentID)

	// Broadcast cancellation as a task:failed event so frontends clear the live card
	s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)
	s.NotifyTaskFinished(task)

	return &CancelTaskResult{
		Task:                 task,
		CancelledChatMessage: cancelledChatMessage,
	}, nil
}

// CancelQueuedChatTasks atomically cancels every queued follow-up in a chat
// session. The session lock preserves the delete path's session -> agent -> task
// order; the agent lock then prevents ClaimTask from promoting a row mid-update.
func (s *TaskService) CancelQueuedChatTasks(ctx context.Context, sessionID, agentID pgtype.UUID) error {
	var tasks []db.AgentTaskQueue
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if _, err := qtx.LockChatSessionForDelete(ctx, sessionID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("lock chat session: %w", err)
		}
		if _, err := qtx.GetAgentForClaimUpdate(ctx, agentID); err != nil {
			return fmt.Errorf("lock chat agent: %w", err)
		}
		var err error
		tasks, err = qtx.CancelQueuedAgentTasksForSession(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("cancel queued chat tasks: %w", err)
		}
		for _, task := range tasks {
			if _, err := s.settleQueuedChatInput(ctx, qtx, task, "remove"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	for _, task := range tasks {
		slog.Info("task cancelled", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(task.IssueID))
		s.captureTaskCancelled(ctx, task)
	}
	if len(tasks) > 0 {
		s.ReconcileAgentStatus(ctx, agentID)
	}
	for _, task := range tasks {
		s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)
	}
	s.notifyTasksFinished(tasks)
	return nil
}

// lockChatSessionForTaskWrite takes the chat_session row a task belongs to. It
// must be the FIRST statement of any transaction that ends up holding both that
// session row and the task's own row, which is every terminal-state path a chat
// task has: complete, fail, cancel, the cancelled-turn finalize, and the
// daemon's mid-flight pin.
//
// chat_session -> agent_task_queue is the repo-wide order (see
// LockChatSessionForTask in chat.sql). DeleteChatSession and
// FinalizeDeferredCancelledChat already took it; the terminal reports did not,
// because they only ever wrote the task row first and the session second and
// nothing else contended for both. Once the cancel and pin paths started
// holding the session row too, "task first" and "session first" existed side by
// side and any crossing pair could deadlock — PostgreSQL aborts one with 40P01
// and runInTx has no retry. There is no per-path fix for that: either every
// writer agrees or none of them are safe.
//
// ErrNoRows means there is nothing to lock — a non-chat task, or one whose
// session was already deleted (the FK NULLs the column) — and the caller then
// has no session write to protect either.
func lockChatSessionForTaskWrite(ctx context.Context, qtx *db.Queries, taskID pgtype.UUID) error {
	if _, err := qtx.LockChatSessionForTask(ctx, taskID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock chat session for task write: %w", err)
	}
	return nil
}

// chatInputOwnerID resolves the id the task's user-message input batch is
// keyed on: chat_input_task_id when set (auto-retry clones inherit their
// parent's, so provenance checks reach the parent's sealed messages), falling
// back to the task's own id for legacy rows.
func chatInputOwnerID(task db.AgentTaskQueue) pgtype.UUID {
	if task.ChatInputTaskID.Valid {
		return task.ChatInputTaskID
	}
	return task.ID
}

// createAssistantChatMessage writes an assistant outcome and reanchors the
// newly-visible queued direct head in the caller's transaction. Keeping the two
// statements together is the transcript-order boundary: readers must not see
// the reply commit while the next head still carries its older enqueue time.
// The caller MUST observe the settling task outside the visible-head status set
// before invoking this helper; completion and failure settle it earlier in the
// same transaction, while cancellation commits that status in its prior
// transaction (#5219). Otherwise the head query still selects the settling task
// and reanchoring its successor is a no-op.
func createAssistantChatMessage(ctx context.Context, qtx *db.Queries, params db.CreateChatMessageParams) (db.ChatMessage, error) {
	if params.Role != "assistant" {
		return db.ChatMessage{}, fmt.Errorf("create assistant chat message: invalid role %q", params.Role)
	}
	row, err := qtx.CreateChatMessage(ctx, params)
	if err != nil {
		return db.ChatMessage{}, err
	}
	if err := qtx.ReanchorNextQueuedDirectChatInput(ctx, db.ReanchorNextQueuedDirectChatInputParams{
		AssistantCreatedAt: row.CreatedAt,
		ChatSessionID:      row.ChatSessionID,
	}); err != nil {
		return db.ChatMessage{}, fmt.Errorf("reanchor next queued direct chat input: %w", err)
	}
	return row, nil
}

func (s *TaskService) settleQueuedChatInput(
	ctx context.Context,
	qtx *db.Queries,
	task db.AgentTaskQueue,
	action string,
) (*CancelledChatMessageResult, error) {
	if !task.ChatSessionID.Valid {
		return nil, nil
	}
	inputOwnerID := chatInputOwnerID(task)
	channelIngested, err := qtx.TaskHasChannelIngestedMessages(ctx, inputOwnerID)
	if err != nil {
		return nil, fmt.Errorf("check queued chat channel provenance: %w", err)
	}
	if channelIngested {
		if _, err := createAssistantChatMessage(ctx, qtx, db.CreateChatMessageParams{
			ID:            dbid.NewV7(),
			ChatSessionID: task.ChatSessionID,
			Role:          "assistant",
			Content:       "Stopped.",
			TaskID:        task.ID,
			ElapsedMs:     computeChatElapsedMs(task),
		}); err != nil {
			return nil, fmt.Errorf("create cancelled queued chat message: %w", err)
		}
		return nil, nil
	}

	var detached []db.Attachment
	if action == "edit" {
		detached, err = qtx.DetachAttachmentsFromUserChatMessageByTask(ctx, inputOwnerID)
		if err != nil {
			return nil, fmt.Errorf("detach edited queued chat attachments: %w", err)
		}
	}
	deleted, err := deleteUserChatInput(ctx, qtx, inputOwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("delete queued chat input: %w", err)
	}
	cancelled := &CancelledChatMessageResult{
		ChatSessionID: util.UUIDToString(deleted.ChatSessionID),
		MessageID:     util.UUIDToString(deleted.ID),
		Content:       deleted.Content,
	}
	if action == "remove" {
		return cancelled, nil
	}

	attachmentIDs := make([]pgtype.UUID, 0, len(detached))
	for _, attachment := range detached {
		attachmentIDs = append(attachmentIDs, attachment.ID)
	}
	if _, err := qtx.CreateChatDraftRestore(ctx, db.CreateChatDraftRestoreParams{
		ID:            deleted.ID,
		ChatSessionID: task.ChatSessionID,
		TaskID:        task.ID,
		Content:       deleted.Content,
		AttachmentIds: attachmentIDs,
	}); err != nil {
		return nil, fmt.Errorf("create queued chat draft restore: %w", err)
	}
	cancelled.RestoreToInput = true
	cancelled.Attachments = detached
	return cancelled, nil
}

// deleteUserChatInput removes a cancelled/edited turn's member-typed input and
// releases any onboarding kickoff the turn had adopted — onto the session's
// next queued turn when there is one, otherwise back to unowned (MUL-5827).
// The two must happen together: deleting the input while leaving the kickoff
// bound to the dead task would strand the onboarding context on a run that
// will never happen.
func deleteUserChatInput(ctx context.Context, qtx *db.Queries, inputOwnerID pgtype.UUID) (db.ChatMessage, error) {
	if err := qtx.ReleaseOnboardingKickoffFromTask(ctx, inputOwnerID); err != nil {
		return db.ChatMessage{}, fmt.Errorf("release onboarding kickoff: %w", err)
	}
	return qtx.DeleteUserChatMessageByTask(ctx, inputOwnerID)
}

func (s *TaskService) finalizeCancelledChatMessage(ctx context.Context, task db.AgentTaskQueue, opts CancelTaskOptions) *CancelledChatMessageResult {
	if !task.ChatSessionID.Valid {
		return nil
	}
	var cancelled *CancelledChatMessageResult
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		// Same protocol as every other terminal path: this transaction marks the
		// task row and then writes chat_message rows, whose FK takes a KEY SHARE
		// lock on the session — two rows again, so it takes the session first.
		if err := lockChatSessionForTaskWrite(ctx, qtx, task.ID); err != nil {
			return err
		}
		messages, err := qtx.ListTaskMessages(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("list cancelled chat task messages: %w", err)
		}
		restorable := len(messages) == 0
		if restorable {
			// Channel-ingested user messages are the durable record of what
			// the platform sender wrote — the sender has no Multica composer
			// to restore a draft into. The gate is the immutable per-message
			// channel_ingested stamp, NOT the channel_chat_session_binding
			// row: archiving a session or rebinding an installation deletes
			// the binding while the messages (and a still-cancellable task)
			// remain. Keyed by the input-batch owner id so an auto-retry
			// clone (which inherits chat_input_task_id) reaches the same
			// verdict as its parent. A channel task settles as "Stopped."
			// below instead of deleting its sealed input batch.
			channelIngested, err := qtx.TaskHasChannelIngestedMessages(ctx, chatInputOwnerID(task))
			if err != nil {
				return fmt.Errorf("check cancelled chat channel provenance: %w", err)
			}
			restorable = !channelIngested
		}
		if restorable && task.StartedAt.Valid && opts.ClientSupportsDraftRestore {
			// A started task's daemon learns of the cancellation by polling
			// and may still be flushing its transcript tail, so "empty" is
			// not trustworthy yet. Defer the judgment until the daemon acks
			// its flush (cancel-ack) or the sweeper grace period expires
			// (#5219). "Non-empty" needs no deferral: late rows only append.
			//
			// Deferring is gated on the client: clients and server do not
			// upgrade together, and a client that cannot read the durable
			// restore would take an empty cancel response as "nothing to put
			// back" and lose the prompt. Such a client falls through to the
			// legacy synchronous branch below — it keeps the pre-#5219 race
			// (an in-flight transcript tail can still be misjudged as empty),
			// which is exactly the behaviour it has against an old server, and
			// strictly better than dropping the input.
			if _, err := qtx.MarkChatFinalizeDeferred(ctx, task.ID); err != nil {
				return fmt.Errorf("mark chat finalize deferred: %w", err)
			}
			return nil
		}
		if restorable {
			inputOwnerID := chatInputOwnerID(task)
			// Detach attachments BEFORE deleting the user message — the
			// attachment FK is ON DELETE CASCADE, so deleting first would
			// destroy rows the restored draft needs to re-bind.
			detached, err := qtx.DetachAttachmentsFromUserChatMessageByTask(ctx, inputOwnerID)
			if err != nil {
				return fmt.Errorf("detach cancelled chat message attachments: %w", err)
			}
			deleted, err := deleteUserChatInput(ctx, qtx, inputOwnerID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("delete empty cancelled chat user message: %w", err)
			}
			// Always restorable now: the delete cannot return a kickoff row, so
			// what comes back is always what the member typed (MUL-5827).
			cancelled = &CancelledChatMessageResult{
				ChatSessionID:  util.UUIDToString(deleted.ChatSessionID),
				MessageID:      util.UUIDToString(deleted.ID),
				Content:        deleted.Content,
				RestoreToInput: true,
				Attachments:    detached,
			}
			return nil
		}
		if _, err := createAssistantChatMessage(ctx, qtx, db.CreateChatMessageParams{
			ID:            dbid.NewV7(),
			ChatSessionID: task.ChatSessionID,
			Role:          "assistant",
			Content:       "Stopped.",
			TaskID:        task.ID,
			ElapsedMs:     computeChatElapsedMs(task),
		}); err != nil {
			return fmt.Errorf("create cancelled chat message: %w", err)
		}
		return nil
	}); err != nil {
		slog.Error("failed to finalize cancelled chat message",
			"task_id", util.UUIDToString(task.ID),
			"chat_session_id", util.UUIDToString(task.ChatSessionID),
			"error", err,
		)
		return nil
	}
	return cancelled
}

// FinalizeDeferredCancelledChat settles the empty/non-empty judgment that
// finalizeCancelledChatMessage deferred for a started-but-empty cancelled
// chat task (#5219). Called from the daemon's cancel-ack (transcript flush
// complete) and from the sweeper grace-period fallback; the marker claim is
// atomic, so concurrent callers cannot finalize the same task twice and a
// call with no pending marker is a no-op. The settled outcome is broadcast
// as chat:cancel_finalized since the cancel HTTP response has long returned.
// RebroadcastCancelledTask re-announces an already-cancelled task after a
// post-terminal delivery landed on its row (the cancel-ack's branch name or
// preserved-worktree error). The original task:cancelled broadcast fired at
// cancel time, BEFORE the daemon's ack — clients may have refetched a row
// without the delivery and will not refetch again on their own. Consumers
// treat task:cancelled as idempotent cache invalidation, so a replay is safe.
func (s *TaskService) RebroadcastCancelledTask(ctx context.Context, taskID pgtype.UUID) {
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		slog.Warn("rebroadcast cancelled task: load failed",
			"task_id", util.UUIDToString(taskID), "error", err)
		return
	}
	if task.Status != "cancelled" {
		// A complete/fail callback already announced its own terminal event
		// carrying the row's final fields; nothing stale to refresh.
		return
	}
	s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)
}

func (s *TaskService) FinalizeDeferredCancelledChat(ctx context.Context, taskID pgtype.UUID) bool {
	var (
		task    db.AgentTaskQueue
		payload protocol.ChatCancelFinalizedPayload
		changed bool
		settled bool
	)
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		// Lock the task's chat_session first. chat_draft_restore has no FK
		// (MUL-3515), so the insert below takes no lock of its own on the
		// session — without this, a workspace/agent/session delete that swept
		// the table just before we commit would leave our restore row (holding
		// the user's prompt) orphaned forever. The deleters take the same lock
		// before their sweep, so one of us blocks: either they wait and their
		// sweep sees our row, or we wait and find no session left to restore
		// into. Locking the session BEFORE the task claim also fixes the global
		// lock order (chat_session -> agent_task_queue) that keeps this from
		// deadlocking against the deleters' cascade.
		_, err := qtx.LockChatSessionForTask(ctx, taskID)
		sessionGone := errors.Is(err, pgx.ErrNoRows)
		if err != nil && !sessionGone {
			return fmt.Errorf("lock chat session for deferred finalize: %w", err)
		}

		// Claim the marker inside the settlement tx: a failed settlement then
		// rolls the claim back so the sweeper can retry, instead of leaving the
		// task with a cleared marker and no finalized outcome. The row lock
		// still serializes the daemon ack and the sweeper — the loser's UPDATE
		// blocks until the winner commits, then matches no row (ErrNoRows) — so
		// the same task is never finalized twice.
		claimed, err := qtx.ClaimChatFinalizeDeferred(ctx, taskID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim deferred chat finalize: %w", err)
		}
		changed = true
		task = claimed
		if sessionGone {
			// The session cascaded away (its FK NULLs the column below anyway):
			// there is no transcript to settle and nowhere to put a restore. The
			// claim above still cleared the marker, so the sweeper stops retrying.
			return nil
		}
		if !claimed.ChatSessionID.Valid {
			return nil
		}
		settled = true
		payload.ChatSessionID = util.UUIDToString(claimed.ChatSessionID)
		payload.TaskID = util.UUIDToString(claimed.ID)
		payload.InitiatorUserID = util.UUIDToString(claimed.InitiatorUserID)

		messages, err := qtx.ListTaskMessages(ctx, claimed.ID)
		if err != nil {
			return fmt.Errorf("list cancelled chat task messages: %w", err)
		}
		restorable := len(messages) == 0
		if restorable {
			// Same immutable-provenance guard as finalizeCancelledChatMessage:
			// channel tasks never restore-delete their sealed input. The sync
			// path no longer defers such tasks; this covers markers created by
			// an older replica during a rolling deploy.
			channelIngested, err := qtx.TaskHasChannelIngestedMessages(ctx, chatInputOwnerID(claimed))
			if err != nil {
				return fmt.Errorf("check cancelled chat channel provenance: %w", err)
			}
			restorable = !channelIngested
		}
		if restorable {
			inputOwnerID := chatInputOwnerID(claimed)
			// The transcript stayed empty through the daemon flush: same
			// outcome as the synchronous empty branch, but the cancel HTTP
			// response is long gone and the broadcast is best-effort. The
			// restore is persisted in this same tx and served by the
			// creator-authorized draft-restores endpoint, so a client that
			// misses the event recovers it on the next session open; the
			// event itself carries no content and is only an invalidation
			// hint.
			detached, err := qtx.DetachAttachmentsFromUserChatMessageByTask(ctx, inputOwnerID)
			if err != nil {
				return fmt.Errorf("detach cancelled chat message attachments: %w", err)
			}
			deleted, err := deleteUserChatInput(ctx, qtx, inputOwnerID)
			if errors.Is(err, pgx.ErrNoRows) {
				payload.Outcome = ""
				return nil
			}
			if err != nil {
				return fmt.Errorf("delete empty cancelled chat user message: %w", err)
			}
			attachmentIDs := make([]pgtype.UUID, 0, len(detached))
			for _, a := range detached {
				attachmentIDs = append(attachmentIDs, a.ID)
			}
			if _, err := qtx.CreateChatDraftRestore(ctx, db.CreateChatDraftRestoreParams{
				ID:            deleted.ID,
				ChatSessionID: claimed.ChatSessionID,
				TaskID:        claimed.ID,
				Content:       deleted.Content,
				AttachmentIds: attachmentIDs,
			}); err != nil {
				return fmt.Errorf("create chat draft restore: %w", err)
			}
			payload.Outcome = protocol.ChatCancelOutcomeRestored
			payload.MessageID = util.UUIDToString(deleted.ID)
			return nil
		}
		row, err := createAssistantChatMessage(ctx, qtx, db.CreateChatMessageParams{
			ID:            dbid.NewV7(),
			ChatSessionID: claimed.ChatSessionID,
			Role:          "assistant",
			Content:       "Stopped.",
			TaskID:        claimed.ID,
			ElapsedMs:     computeChatElapsedMs(claimed),
		})
		if err != nil {
			return fmt.Errorf("create cancelled chat message: %w", err)
		}
		payload.Outcome = protocol.ChatCancelOutcomeStopped
		payload.MessageID = util.UUIDToString(row.ID)
		payload.Content = row.Content
		payload.MessageKind = row.MessageKind
		if row.CreatedAt.Valid {
			payload.CreatedAt = row.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if row.ElapsedMs.Valid {
			payload.ElapsedMs = row.ElapsedMs.Int64
		}
		return nil
	}); err != nil {
		slog.Error("failed to finalize deferred cancelled chat",
			"task_id", util.UUIDToString(taskID),
			"error", err,
		)
		return false
	}
	if !settled || payload.Outcome == "" {
		return changed
	}
	s.broadcastChatCancelFinalized(ctx, task, payload)
	return changed
}

func (s *TaskService) broadcastChatCancelFinalized(ctx context.Context, task db.AgentTaskQueue, payload protocol.ChatCancelFinalizedPayload) {
	workspaceID := s.ResolveTaskWorkspaceID(ctx, task)
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(events.Event{
		Type:          protocol.EventChatCancelFinalized,
		WorkspaceID:   workspaceID,
		ActorType:     "system",
		ActorID:       "",
		ChatSessionID: util.UUIDToString(task.ChatSessionID),
		Payload:       payload,
	})
}

// ClaimTask atomically claims the next queued task for an agent on its current
// runtime, respecting max_concurrent_tasks.
func (s *TaskService) ClaimTask(ctx context.Context, agentID pgtype.UUID) (*db.AgentTaskQueue, error) {
	return s.claimTask(ctx, agentID, pgtype.UUID{})
}

// claimTask is the runtime-scoped claim primitive used by daemon poll paths.
// The exported ClaimTask wrapper omits runtimeID and therefore resolves the
// agent's currently bound runtime. Scoping the SQL claim itself prevents an
// offline candidate on runtime A from causing the same agent's task on runtime
// B to be dispatched and then dropped by the caller's runtime guard.
func (s *TaskService) claimTask(ctx context.Context, agentID, runtimeID pgtype.UUID) (*db.AgentTaskQueue, error) {
	start := time.Now()
	outcome := "unknown"
	var getAgentMs, countRunningMs, claimAgentMs, reanchorMs, updateStatusMs, dispatchMs int64
	var claimed *db.AgentTaskQueue
	var reclaimCheckAfter time.Time
	defer func() {
		s.maybeLogClaimSlow(agentID, outcome, start, getAgentMs, countRunningMs, claimAgentMs, reanchorMs, updateStatusMs, dispatchMs)
	}()

	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		t0 := time.Now()
		agent, err := qtx.GetAgentForClaimUpdate(ctx, agentID)
		getAgentMs = time.Since(t0).Milliseconds()
		if err != nil {
			outcome = "error_get_agent"
			return fmt.Errorf("agent not found: %w", err)
		}
		claimRuntimeID := runtimeID
		if !claimRuntimeID.Valid {
			claimRuntimeID = agent.RuntimeID
		}
		if !claimRuntimeID.Valid {
			outcome = "no_runtime"
			return nil
		}
		// A daemon may still hold a stale candidate after the agent is rebound.
		// Reject it before doing capacity work. ClaimAgentTask repeats the fence
		// before its state transition; the claim handler then rechecks the freshly
		// loaded Agent before returning any payload. Runtime mutation teardown is
		// responsible for serializing and settling the remaining queued rows.
		if runtimeID.Valid && agent.RuntimeID != runtimeID {
			outcome = "runtime_mismatch"
			return nil
		}

		t0 = time.Now()
		running, err := qtx.CountRunningTasks(ctx, agentID)
		countRunningMs = time.Since(t0).Milliseconds()
		if err != nil {
			outcome = "error_count_running"
			return fmt.Errorf("count running tasks: %w", err)
		}
		if running >= int64(agent.MaxConcurrentTasks) {
			slog.Debug("task claim: no capacity", "agent_id", util.UUIDToString(agentID), "running", running, "max", agent.MaxConcurrentTasks)
			outcome = "no_capacity"
			return nil
		}

		t0 = time.Now()
		reclaimCheckAfter = t0.Add(claimResponseRecoveryWindow + ReclaimCheckHintSafetyMargin)
		task, err := qtx.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{
			AgentID:          agentID,
			RuntimeID:        claimRuntimeID,
			PrepareLeaseSecs: prepareLeaseDuration.Seconds(),
			RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
		})
		claimAgentMs = time.Since(t0).Milliseconds()
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.Debug("task claim: no tasks available", "agent_id", util.UUIDToString(agentID))
				outcome = "no_tasks"
				return nil
			}
			outcome = "error_claim"
			return fmt.Errorf("claim task: %w", err)
		}

		// An idle task-owned direct-chat row may already be visible as the
		// positional queue head. Normal completion reanchors a successor beside
		// the assistant outcome before commit; this claim-time query is the
		// compatibility fallback for an older or otherwise out-of-order row.
		// It only moves that row inside the claim transaction, so full-list and
		// cursor readers see the corrected boundary once it is dispatched. Retry
		// children keep the root input owner and stale-dispatch reclaim uses a
		// separate query, so neither path moves an already-visible turn again.
		if task.ChatSessionID.Valid && task.ChatInputTaskID.Valid && task.ChatInputTaskID == task.ID {
			t0 = time.Now()
			if err := qtx.ReanchorClaimedDirectChatInput(ctx, db.ReanchorClaimedDirectChatInputParams{
				DispatchedAt: task.DispatchedAt,
				TaskID:       task.ID,
			}); err != nil {
				reanchorMs = time.Since(t0).Milliseconds()
				outcome = "error_reanchor_chat_input"
				return fmt.Errorf("reanchor claimed direct chat input: %w", err)
			}
			reanchorMs = time.Since(t0).Milliseconds()
		}

		claimedTask := task
		claimed = &claimedTask
		return nil
	})
	if err != nil {
		if outcome == "unknown" {
			outcome = "error_transaction"
		}
		return nil, err
	}
	if claimed == nil {
		return nil, nil
	}
	s.trackTaskForReclaim(*claimed, reclaimCheckAfter)

	slog.Info("task claimed", "task_id", util.UUIDToString(claimed.ID), "agent_id", util.UUIDToString(agentID))
	s.captureTaskDispatched(ctx, *claimed)

	// Refresh agent status from active tasks. This avoids a stale unconditional
	// working write racing after a just-cancelled claim.
	t0 := time.Now()
	s.ReconcileAgentStatus(ctx, agentID)
	updateStatusMs = time.Since(t0).Milliseconds()

	// Broadcast task:dispatch. ResolveTaskWorkspaceID inside this path can
	// re-query issue/chat_session/autopilot_run, so it can also be a real
	// contributor to claim latency.
	t0 = time.Now()
	s.broadcastTaskDispatch(ctx, *claimed)
	dispatchMs = time.Since(t0).Milliseconds()

	outcome = "claimed"
	return claimed, nil
}

// ClaimTaskForRuntime claims the next runnable task for a runtime while
// still respecting each agent's max_concurrent_tasks limit.
//
// Empty-claim fast path: when EmptyClaim is configured and a recent
// check verified the runtime had no queued tasks, returns immediately
// without touching Postgres. The cache is invalidated synchronously on
// every enqueue (notifyTaskAvailable), so a queued task becomes
// claimable on the next call rather than waiting for the TTL.
func (s *TaskService) ClaimTaskForRuntime(ctx context.Context, runtimeID pgtype.UUID) (*db.AgentTaskQueue, error) {
	start := time.Now()
	var (
		outcome          = "no_task"
		listMs, loopMs   int64
		listCount, tried int
		claimedFlag      bool
	)
	defer func() {
		totalMs := time.Since(start).Milliseconds()
		if totalMs < 300 {
			return
		}
		slog.Info("claim_for_runtime slow",
			"runtime_id", util.UUIDToString(runtimeID),
			"outcome", outcome,
			"total_ms", totalMs,
			"list_pending_ms", listMs,
			"list_pending_count", listCount,
			"agents_tried", tried,
			"claim_loop_ms", loopMs,
			"claimed", claimedFlag,
		)
	}()

	runtimeKey := util.UUIDToString(runtimeID)
	if err := s.PromoteDueDeferredTasksForRuntime(ctx, runtimeID); err != nil {
		outcome = "error_promote_deferred"
		return nil, err
	}

	// Keep stale-response recovery before EmptyClaim because the queued-only
	// cache cannot represent dispatched work. ReclaimCheck independently skips
	// the UPDATE until a task hint or bounded DB backstop is due.
	checkStarted := time.Now()
	if due := s.ReclaimCheck.DueRuntimeIDs(ctx, []string{runtimeKey}, checkStarted); len(due) > 0 {
		reclaimCheckAfter := time.Now().Add(claimResponseRecoveryWindow + ReclaimCheckHintSafetyMargin)
		stale, err := s.Queries.ReclaimStaleDispatchedTaskForRuntime(ctx, db.ReclaimStaleDispatchedTaskForRuntimeParams{
			RuntimeID:         runtimeID,
			ClaimRecoverySecs: claimResponseRecoveryWindow.Seconds(),
			PrepareLeaseSecs:  prepareLeaseDuration.Seconds(),
			RuntimeStaleSecs:  RuntimeClaimFreshnessSeconds,
		})
		if err == nil {
			s.ReclaimCheck.MarkChecked(
				ctx,
				[]string{runtimeKey},
				checkStarted,
				time.Now().Add(ReclaimCheckRetryInterval),
			)
			s.trackTaskForReclaim(stale, reclaimCheckAfter)
			outcome = "reclaimed_dispatched"
			claimedFlag = true
			slog.Info("stale dispatched task reclaimed",
				"task_id", util.UUIDToString(stale.ID),
				"runtime_id", runtimeKey,
				"agent_id", util.UUIDToString(stale.AgentID),
			)
			return &stale, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			outcome = "error_reclaim_dispatched"
			return nil, fmt.Errorf("reclaim stale dispatched task: %w", err)
		}
		s.ReclaimCheck.MarkChecked(
			ctx,
			[]string{runtimeKey},
			checkStarted,
			time.Now().Add(ReclaimCheckRetryInterval),
		)
	}

	if s.EmptyClaim.IsEmpty(ctx, runtimeKey) {
		outcome = "empty_cache_hit"
		return nil, nil
	}

	// Sample the invalidation version BEFORE the SELECT. If a
	// concurrent enqueue Bumps between this read and the post-SELECT
	// MarkEmpty, the next IsEmpty will see the empty key tagged with
	// a stale version and reject it — closing the race that would
	// otherwise stall the just-queued task until the empty key's TTL
	// expired.
	preSelectVersion := s.EmptyClaim.CurrentVersion(ctx, runtimeKey)

	t0 := time.Now()
	tasks, err := s.Queries.ListQueuedClaimCandidatesByRuntime(ctx, runtimeID)
	listMs = time.Since(t0).Milliseconds()
	listCount = len(tasks)
	if err != nil {
		outcome = "error_list"
		return nil, fmt.Errorf("list queued claim candidates: %w", err)
	}

	if len(tasks) == 0 {
		s.EmptyClaim.MarkEmpty(ctx, runtimeKey, preSelectVersion)
		outcome = "empty_db"
		return nil, nil
	}

	loopStart := time.Now()
	triedAgents := map[string]struct{}{}
	var claimed *db.AgentTaskQueue
	for _, candidate := range tasks {
		agentKey := util.UUIDToString(candidate.AgentID)
		if _, seen := triedAgents[agentKey]; seen {
			continue
		}
		triedAgents[agentKey] = struct{}{}
		tried++

		task, err := s.claimTask(ctx, candidate.AgentID, runtimeID)
		if err != nil {
			loopMs = time.Since(loopStart).Milliseconds()
			outcome = "error_claim"
			return nil, err
		}
		if task != nil && task.RuntimeID == runtimeID {
			claimed = task
			break
		}
	}
	loopMs = time.Since(loopStart).Milliseconds()
	if claimed != nil {
		claimedFlag = true
		outcome = "claimed"
	}

	return claimed, nil
}

// FinalizeTaskClaim atomically persists the task-scoped agent token, an
// optional short-lived daemon token used by the Remote MCP broker, and, for a
// comment-backed task, the exact comment ids embedded in the response. The
// handler must call this only after the full payload has been built and before
// writing any response bytes. A failure rolls every write back so the claim can
// be safely returned to the queue.
func (s *TaskService) FinalizeTaskClaim(
	ctx context.Context,
	task db.AgentTaskQueue,
	token db.CreateTaskTokenParams,
	deliveredCommentIDs []pgtype.UUID,
	recordCommentReceipt bool,
	daemonTokens ...db.CreateDaemonTokenParams,
) ([]pgtype.UUID, error) {
	if len(daemonTokens) > 1 {
		return nil, fmt.Errorf("finalize task claim: expected at most one daemon token, got %d", len(daemonTokens))
	}
	receipt := task.DeliveredCommentIds
	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if _, err := qtx.CreateTaskToken(ctx, token); err != nil {
			return fmt.Errorf("create task token: %w", err)
		}
		if len(daemonTokens) == 1 {
			// Opportunistic bounded cleanup keeps short-lived per-task daemon
			// credentials from accumulating without adding another sweeper.
			if err := qtx.DeleteExpiredDaemonTokens(ctx); err != nil {
				return fmt.Errorf("delete expired daemon tokens: %w", err)
			}
			if _, err := qtx.CreateDaemonToken(ctx, daemonTokens[0]); err != nil {
				return fmt.Errorf("create remote MCP daemon token: %w", err)
			}
		}
		if !recordCommentReceipt {
			return nil
		}
		persisted, err := qtx.SetTaskDeliveredCommentIDs(ctx, db.SetTaskDeliveredCommentIDsParams{
			DeliveredCommentIds:      deliveredCommentIDs,
			TaskID:                   task.ID,
			RuntimeID:                task.RuntimeID,
			DispatchedAt:             task.DispatchedAt,
			ExpectedTriggerCommentID: task.TriggerCommentID,
		})
		if err != nil {
			return fmt.Errorf("set delivered comment ids: %w", err)
		}
		receipt = persisted
		return nil
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

// RequeueTaskAfterClaimFailure immediately releases an exact dispatched claim
// whose payload finalization failed before the HTTP response was written. The
// SQL CAS includes dispatched_at so a late handler cannot roll back a newer
// reclaim. This is not a fresh enqueue: do not duplicate queued analytics.
func (s *TaskService) RequeueTaskAfterClaimFailure(ctx context.Context, task db.AgentTaskQueue) (*db.AgentTaskQueue, error) {
	requeued, err := s.Queries.RequeueAgentTaskAfterClaimFailure(ctx, db.RequeueAgentTaskAfterClaimFailureParams{
		TaskID:       task.ID,
		RuntimeID:    task.RuntimeID,
		DispatchedAt: task.DispatchedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("requeue task after claim failure: %w", err)
	}
	s.forgetTaskReclaim(requeued)
	s.ReconcileAgentStatus(ctx, requeued.AgentID)
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, requeued)
	s.notifyTaskAvailable(requeued)
	slog.Info("task requeued after claim finalization failure",
		"task_id", util.UUIDToString(requeued.ID),
		"runtime_id", util.UUIDToString(requeued.RuntimeID),
	)
	return &requeued, nil
}

// ClaimTasksForRuntimes is the machine-level (MUL-4257) batch counterpart of
// ClaimTaskForRuntime: it claims up to maxTasks tasks across every runtime in
// runtimeIDs in a single call, so a daemon can poll for all of its runtimes
// with one HTTP request and a constant number of DB queries instead of one
// request (and one promote/reclaim/list cycle) per runtime.
//
// It preserves the exact per-runtime semantics, just set-ified:
//  1. promote due deferred tasks across the set (one UPDATE);
//  2. reclaim up to maxTasks stale-dispatched tasks across the set (one UPDATE)
//     — done before the empty-cache check because a lost claim response moves
//     the task out of `queued`, which the empty-queued cache cannot represent;
//  3. short-circuit runtimes whose empty-claim verdict is cached, sampling the
//     invalidation version for the rest BEFORE the candidate SELECT;
//  4. list queued candidates across the non-empty set (one SELECT);
//  5. mark still-empty runtimes so their next idle poll skips Postgres;
//  6. claim per distinct agent via the runtime-scoped helper, preserving
//     per-(issue, agent) serialization, the agent concurrency cap, and every
//     dispatch side effect until maxTasks is reached.
//
// The returned slice contains both reclaimed and freshly-claimed tasks, each
// already carrying its runtime_id so the daemon routes it to the matching
// runtime locally.
func (s *TaskService) ClaimTasksForRuntimes(ctx context.Context, runtimeIDs []pgtype.UUID, maxTasks int) ([]db.AgentTaskQueue, error) {
	if len(runtimeIDs) == 0 || maxTasks <= 0 {
		return nil, nil
	}

	// De-dup runtime IDs defensively so MarkEmpty/version bookkeeping stays
	// unambiguous even if a daemon ever sends a duplicate.
	seen := make(map[string]struct{}, len(runtimeIDs))
	uniqueIDs := make([]pgtype.UUID, 0, len(runtimeIDs))
	runtimeInSet := make(map[string]struct{}, len(runtimeIDs))
	for _, rid := range runtimeIDs {
		key := util.UUIDToString(rid)
		runtimeInSet[key] = struct{}{}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		uniqueIDs = append(uniqueIDs, rid)
	}

	claimed := make([]db.AgentTaskQueue, 0, maxTasks)

	// 1. Promote due deferred tasks across the whole set (promote-first, like
	// the singular path). Replay the per-row side effects the singular service
	// method PromoteDueDeferredTasksForRuntime performs — crucially
	// EmptyClaim.Bump (via NotifyTaskEnqueued → notifyTaskAvailable) so a
	// just-promoted deferred task invalidates its runtime's cached empty
	// verdict BEFORE the empty-cache filter in step 3; otherwise a stale
	// MarkEmpty from a prior idle poll would short-circuit the runtime and the
	// promoted task would sit unclaimed until the empty key's TTL. Also emits
	// the deferred→queued UI event and the enqueue analytics sample.
	s.cancelSupersededDeferredRetries(ctx, uniqueIDs)
	promoted, err := s.Queries.PromoteDueDeferredTasksForRuntimes(ctx, db.PromoteDueDeferredTasksForRuntimesParams{
		RuntimeIds:       uniqueIDs,
		RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
	})
	if isDuplicatePendingTaskErr(err) {
		// Same tolerance as the single-runtime path, and it matters more here:
		// one contended row would otherwise fail the claim for EVERY runtime in
		// the batch. Promote nothing this tick and let the claim continue.
		slog.Info("promote deferred tasks (batch): slot taken by a concurrent enqueue, skipping this tick")
		promoted = nil
	} else if err != nil {
		return nil, fmt.Errorf("promote deferred tasks: %w", err)
	}
	for _, task := range promoted {
		slog.Info("deferred fallback task promoted (batch)",
			"task_id", util.UUIDToString(task.ID),
			"runtime_id", util.UUIDToString(task.RuntimeID),
			"agent_id", util.UUIDToString(task.AgentID),
		)
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
		s.NotifyTaskEnqueued(ctx, task)
	}

	// 2. Reclaim lost-response dispatched tasks when any runtime's task schedule
	// or bounded DB backstop is due. The query always receives the complete
	// machine-level set so per-runtime backstops stay aligned and fixed UPDATE
	// setup/locking cost is paid at a predictable cadence. A nil/missing/failed
	// cache preserves the historical query path.
	runtimeKeys := make([]string, 0, len(uniqueIDs))
	for _, rid := range uniqueIDs {
		runtimeKeys = append(runtimeKeys, util.UUIDToString(rid))
	}
	checkStarted := time.Now()
	dueKeys := s.ReclaimCheck.DueRuntimeIDs(ctx, runtimeKeys, checkStarted)
	var reclaimed []db.AgentTaskQueue
	var reclaimCheckAfter time.Time
	if len(dueKeys) > 0 {
		reclaimCheckAfter = time.Now().Add(claimResponseRecoveryWindow + ReclaimCheckHintSafetyMargin)
		reclaimed, err = s.Queries.ReclaimStaleDispatchedTasksForRuntimes(ctx, db.ReclaimStaleDispatchedTasksForRuntimesParams{
			RuntimeIds:        uniqueIDs,
			ClaimRecoverySecs: claimResponseRecoveryWindow.Seconds(),
			PrepareLeaseSecs:  prepareLeaseDuration.Seconds(),
			RuntimeStaleSecs:  RuntimeClaimFreshnessSeconds,
			MaxTasks:          int32(maxTasks),
		})
		if err != nil {
			return nil, fmt.Errorf("reclaim stale dispatched tasks: %w", err)
		}
		// The UPDATE's fixed setup/locking cost dominates array width. Query and
		// advance the complete machine-level set together so per-runtime backstops
		// cannot drift into one UPDATE on almost every daemon poll.
		s.ReclaimCheck.MarkChecked(
			ctx,
			runtimeKeys,
			checkStarted,
			time.Now().Add(ReclaimCheckRetryInterval),
		)
	}
	for i := range reclaimed {
		s.trackTaskForReclaim(reclaimed[i], reclaimCheckAfter)
		claimed = append(claimed, reclaimed[i])
		slog.Info("stale dispatched task reclaimed (batch)",
			"task_id", util.UUIDToString(reclaimed[i].ID),
			"runtime_id", util.UUIDToString(reclaimed[i].RuntimeID),
			"agent_id", util.UUIDToString(reclaimed[i].AgentID),
		)
	}
	if len(claimed) >= maxTasks {
		return claimed[:maxTasks], nil
	}

	// 3. Empty-cache short-circuit + version sampling for the remaining runtimes.
	nonEmpty := make([]pgtype.UUID, 0, len(uniqueIDs))
	versions := make(map[string]int64, len(uniqueIDs))
	for _, rid := range uniqueIDs {
		key := util.UUIDToString(rid)
		if s.EmptyClaim.IsEmpty(ctx, key) {
			continue
		}
		versions[key] = s.EmptyClaim.CurrentVersion(ctx, key)
		nonEmpty = append(nonEmpty, rid)
	}
	if len(nonEmpty) == 0 {
		return claimed, nil
	}

	// 4. One candidate SELECT across the non-empty set.
	candidates, err := s.Queries.ListQueuedClaimCandidatesByRuntimes(ctx, nonEmpty)
	if err != nil {
		// Steps 2/6 commit reclaimed/claimed tasks in their own transactions,
		// so `claimed` may already hold tasks dispatched server-side. Dropping
		// them with a 500 makes the daemon HTTP-fall-back and claim a SECOND
		// batch into the same free slots (the first batch then waits for stale
		// reclaim) — the same double-claim this PR set out to remove
		// (MUL-4257). Prefer partial success: hand back what committed so the
		// handler finalizes and returns it; the errored candidates stay queued
		// for the next poll.
		if len(claimed) > 0 {
			slog.Error("batch claim: candidate query failed after partial success; returning claimed tasks to avoid loss",
				"error", err, "claimed", len(claimed))
			return claimed, nil
		}
		return nil, fmt.Errorf("list queued claim candidates: %w", err)
	}

	// 5. Mark runtimes with zero candidates empty so their next idle poll skips
	// Postgres. Runtimes that had at least one candidate are intentionally not
	// marked (positive results always re-check the DB, matching the singular
	// path).
	withCandidates := make(map[string]struct{}, len(candidates))
	for i := range candidates {
		withCandidates[util.UUIDToString(candidates[i].RuntimeID)] = struct{}{}
	}
	for _, rid := range nonEmpty {
		key := util.UUIDToString(rid)
		if _, ok := withCandidates[key]; !ok {
			s.EmptyClaim.MarkEmpty(ctx, key, versions[key])
		}
	}

	// 6. Claim per distinct agent through the runtime-scoped helper, preserving
	// per-(issue, agent) serialization, capacity caps, and dispatch side effects.
	triedAgents := make(map[string]struct{}, len(candidates))
	for i := range candidates {
		if len(claimed) >= maxTasks {
			break
		}
		agentKey := util.UUIDToString(candidates[i].AgentID)
		if _, tried := triedAgents[agentKey]; tried {
			continue
		}
		triedAgents[agentKey] = struct{}{}

		task, err := s.claimTask(ctx, candidates[i].AgentID, candidates[i].RuntimeID)
		if err != nil {
			// Each scoped claim commits in its own transaction, so earlier
			// iterations (and step-2 reclaims) are already dispatched
			// server-side. Returning nil here would drop them and force the
			// daemon to double-claim via HTTP fallback (MUL-4257). Return the
			// partial batch instead; the failed agent's task stays queued.
			if len(claimed) > 0 {
				slog.Error("batch claim: claim task failed after partial success; returning claimed tasks to avoid loss",
					"error", err, "claimed", len(claimed))
				return claimed, nil
			}
			return nil, fmt.Errorf("claim task: %w", err)
		}
		if task == nil {
			continue
		}
		// The SQL claim is scoped to the candidate runtime. Retain this guard as
		// a defensive contract check so a future query change cannot route work
		// to a runtime this daemon does not host.
		if _, ok := runtimeInSet[util.UUIDToString(task.RuntimeID)]; !ok {
			continue
		}
		claimed = append(claimed, *task)
	}

	return claimed, nil
}

// cancelSupersededDeferredRetries drops deferred auto-retry rows that an active
// task already supersedes, so a single rerun click still produces exactly one
// more run. Runs immediately before promotion — promotion is the moment a stale
// retry would otherwise become claimable. Best-effort: a failure here must not
// stop the claim loop, it just leaves the row for the next tick.
func (s *TaskService) cancelSupersededDeferredRetries(ctx context.Context, runtimeIDs []pgtype.UUID) {
	if len(runtimeIDs) == 0 {
		return
	}
	cancelled, err := s.Queries.CancelSupersededDeferredRetriesForRuntimes(ctx, runtimeIDs)
	if err != nil {
		slog.Warn("cancel superseded deferred retries failed", "error", err)
		return
	}
	for _, task := range cancelled {
		slog.Info("deferred auto-retry cancelled: superseded by an active task",
			"task_id", util.UUIDToString(task.ID),
			"issue_id", util.UUIDToString(task.IssueID),
			"agent_id", util.UUIDToString(task.AgentID),
		)
		s.captureTaskCancelled(ctx, task)
		s.ReconcileAgentStatus(ctx, task.AgentID)
		s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)
	}
}

func (s *TaskService) PromoteDueDeferredTasksForRuntime(ctx context.Context, runtimeID pgtype.UUID) error {
	s.cancelSupersededDeferredRetries(ctx, []pgtype.UUID{runtimeID})
	tasks, err := s.Queries.PromoteDueDeferredTasksForRuntime(ctx, db.PromoteDueDeferredTasksForRuntimeParams{
		RuntimeID:        runtimeID,
		RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
	})
	if isDuplicatePendingTaskErr(err) {
		// The NOT EXISTS fence inside the query cannot see an enqueue that has
		// not committed yet, so a rerun landing in the same instant still
		// collides here. One row losing its slot must not fail the whole claim:
		// the row stays deferred and the next tick — by which time the rerun is
		// visible — skips it cleanly. Costs one poll interval, never a stall.
		slog.Info("promote due deferred tasks: slot taken by a concurrent enqueue, skipping this tick",
			"runtime_id", util.UUIDToString(runtimeID))
		return nil
	}
	if err != nil {
		return fmt.Errorf("promote due deferred tasks: %w", err)
	}
	for _, task := range tasks {
		slog.Info("deferred fallback task promoted",
			"task_id", util.UUIDToString(task.ID),
			"runtime_id", util.UUIDToString(runtimeID),
			"agent_id", util.UUIDToString(task.AgentID),
		)
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
		s.NotifyTaskEnqueued(ctx, task)
	}
	return nil
}

// maybeLogClaimSlow emits one structured log per ClaimTask call when its total
// latency exceeds 300ms, so the prod tail can be diagnosed without flooding
// logs at normal poll rates. Called via defer so it captures the full path
// including post-claim updateAgentStatus / broadcastTaskDispatch (both of
// which can hit the DB) and any error exit.
func (s *TaskService) maybeLogClaimSlow(agentID pgtype.UUID, outcome string, start time.Time, getAgentMs, countRunningMs, claimAgentMs, reanchorMs, updateStatusMs, dispatchMs int64) {
	totalMs := time.Since(start).Milliseconds()
	if totalMs < 300 {
		return
	}
	slog.Info("claim_task slow",
		"agent_id", util.UUIDToString(agentID),
		"outcome", outcome,
		"total_ms", totalMs,
		"get_agent_ms", getAgentMs,
		"count_running_ms", countRunningMs,
		"claim_agent_ms", claimAgentMs,
		"reanchor_chat_input_ms", reanchorMs,
		"update_status_ms", updateStatusMs,
		"dispatch_ms", dispatchMs,
	)
}

// StartTask transitions a dispatched task to running.
// Issue status is NOT changed here — the agent manages it via the CLI.
func (s *TaskService) StartTask(ctx context.Context, taskID pgtype.UUID) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.StartAgentTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("start task: %w", err)
	}
	s.forgetTaskReclaim(task)
	s.cancelDeferredEscalationsForTask(ctx, task.ID)

	slog.Info("task started", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(task.IssueID))
	s.captureTaskStarted(ctx, task)
	// A local-directory waiter was reconciled out of the persisted working
	// status while parked. Restore working as soon as it enters running; the
	// normal dispatched -> running path is already working, so this is
	// intentionally idempotent there.
	s.ReconcileAgentStatus(ctx, task.AgentID)
	// Tell every connected workspace WS client that this task transitioned
	// (dispatched | waiting_local_directory) → running. Without this, the
	// workspace-wide `agentTaskSnapshot` query only refreshes on the 30s
	// staleTime, so any UI that distinguishes "queued" from "running" (e.g.
	// the issue-card agent activity indicator) lags by up to half a minute
	// on the transition users care about most.
	s.broadcastTaskEvent(ctx, protocol.EventTaskRunning, task)
	return &task, nil
}

func (s *TaskService) cancelDeferredEscalationsForTask(ctx context.Context, taskID pgtype.UUID) {
	cancelled, err := s.Queries.CancelDeferredEscalationsForTask(ctx, taskID)
	if err != nil {
		slog.Warn("cancel deferred escalations for task failed", "task_id", util.UUIDToString(taskID), "error", err)
		return
	}
	for _, task := range cancelled {
		slog.Info("deferred fallback task cancelled",
			"task_id", util.UUIDToString(task.ID),
			"primary_task_id", util.UUIDToString(taskID),
			"reason", "primary_acknowledged",
		)
	}
}

func (s *TaskService) CancelDeferredEscalationsForIssueAgent(ctx context.Context, issueID, agentID pgtype.UUID) {
	cancelled, err := s.Queries.CancelDeferredEscalationsForIssueAgent(ctx, db.CancelDeferredEscalationsForIssueAgentParams{
		IssueID: issueID,
		AgentID: agentID,
	})
	if err != nil {
		slog.Warn("cancel deferred escalations for issue agent failed",
			"issue_id", util.UUIDToString(issueID),
			"agent_id", util.UUIDToString(agentID),
			"error", err)
		return
	}
	for _, task := range cancelled {
		slog.Info("deferred fallback task cancelled",
			"task_id", util.UUIDToString(task.ID),
			"issue_id", util.UUIDToString(issueID),
			"agent_id", util.UUIDToString(agentID),
			"reason", "agent_comment_acknowledged",
		)
	}
}

// ExtendTaskPrepareLease keeps a claimed-but-not-started task protected while
// the daemon resolves cached inputs and prepares the execution environment.
func (s *TaskService) ExtendTaskPrepareLease(ctx context.Context, taskID, runtimeID pgtype.UUID) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.ExtendAgentTaskPrepareLease(ctx, db.ExtendAgentTaskPrepareLeaseParams{
		ID:        taskID,
		RuntimeID: runtimeID,
		LeaseSecs: prepareLeaseDuration.Seconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("extend task prepare lease: %w", err)
	}
	if task.Status == "dispatched" {
		// Use the successful response time as a conservative application-clock
		// approximation of the DB lease start. Scheduling slightly late by query
		// latency is safer than an early failed check suppressing the hint until
		// the next backstop.
		s.extendTaskReclaimHint(task, time.Now().Add(prepareLeaseDuration))
	} else {
		s.forgetTaskReclaim(task)
	}
	return &task, nil
}

// MarkTaskWaitingLocalDirectory parks a dispatched task in the
// waiting_local_directory state while the daemon waits for another in-flight
// task to release the project_resource path lock. reason carries a short
// human-readable hint (typically the contested path) that the UI surfaces
// next to the status. Returns the updated row so the daemon can confirm the
// transition and so the broadcast carries the up-to-date snapshot.
func (s *TaskService) MarkTaskWaitingLocalDirectory(ctx context.Context, taskID pgtype.UUID, reason string) (*db.AgentTaskQueue, error) {
	reason = sanitizeWaitReason(reason)
	task, err := s.Queries.MarkAgentTaskWaitingLocalDirectory(ctx, db.MarkAgentTaskWaitingLocalDirectoryParams{
		ID:               taskID,
		WaitReason:       pgtype.Text{String: reason, Valid: reason != ""},
		PrepareLeaseSecs: prepareLeaseDuration.Seconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("mark task waiting_local_directory: %w", err)
	}
	s.forgetTaskReclaim(task)

	slog.Info("task waiting_local_directory",
		"task_id", util.UUIDToString(task.ID),
		"issue_id", util.UUIDToString(task.IssueID),
		"reason", reason,
	)
	// waiting_local_directory is owned/queued work, not executing work. The
	// claim path marked the agent working while the row was dispatched, so
	// reconcile immediately when it parks instead of leaving that persisted
	// status stale until a terminal transition.
	s.ReconcileAgentStatus(ctx, task.AgentID)
	// Carry the reason on the event, not just in the row. Without it the client
	// learns WHY a task parked only on the follow-up refetch, so the pill spends
	// that round-trip saying nothing useful — which is the gap this hint exists
	// to close. Already sanitized above, so the socket cannot carry a path the
	// REST payload would have withheld.
	extra := map[string]any{}
	if reason != "" {
		extra["wait_reason"] = reason
	}
	s.broadcastTaskEvent(ctx, protocol.EventTaskWaitingLocalDirectory, task, extra)
	return &task, nil
}

// legacyLocalDirectoryWaitPrefix is how daemons before the display-name change
// opened a wait reason: the literal word, a space, then the absolute path.
const legacyLocalDirectoryWaitPrefix = "local_directory "

// sanitizeWaitReason drops a wait reason that carries an absolute filesystem
// path, then trims what remains.
//
// Version skew makes this load-bearing rather than defensive. Current daemons
// send a display name (localDirectoryAssignment.DisplayName) precisely because
// this text reaches every client on the session and lands in screenshots. An
// OLDER daemon paired with this server still sends `local_directory /Users/
// <name>/repo (held by task abc12345)`, and since clients now render the field
// instead of ignoring it, passing that through would put the user's home path
// — and account name — on screen. The leak would be introduced BY surfacing
// the field, so the guard belongs with it.
//
// Dropping the whole string rather than editing it: clients already handle an
// absent reason (older servers never sent one) by showing the plain waiting
// label, which is exactly the pre-change behaviour and is never wrong.
//
// Remove once no supported daemon emits the legacy format.
func sanitizeWaitReason(reason string) string {
	reason = strings.TrimSpace(reason)
	rest, ok := strings.CutPrefix(reason, legacyLocalDirectoryWaitPrefix)
	if ok && startsWithAbsolutePath(rest) {
		return ""
	}
	return reason
}

// startsWithAbsolutePath reports whether s opens with an absolute path on any
// platform the daemon runs on: POSIX (/…), Windows drive (C:\… or C:/…), or a
// UNC share (\\host\share). Checked against the daemon's own output, so the
// question is only "did an old daemon put a path here", not general parsing.
//
// It deliberately does NOT match the holder clause a current daemon appends —
// `local_directory (held by task abc12345)` is what this server produces when
// a directory is genuinely NAMED "local_directory", and that name is the user's
// own label, not a path.
func startsWithAbsolutePath(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '/' || strings.HasPrefix(s, `\\`) {
		return true
	}
	// Drive-letter form: a single ASCII letter, a colon, then a separator.
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		c := s[0]
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return false
}

// CompleteTask marks a task as completed.
// Issue status is NOT changed here — the agent manages it via the CLI.
//
// For chat tasks, CompleteAgentTask and the chat_session resume-pointer
// update run in a single transaction. This closes a race where the next
// queued chat message could be claimed in the window between the task
// flipping to 'completed' and chat_session.session_id being refreshed,
// causing the new task to resume against a stale (or NULL) session.
// durableWorkDir is terminal delivery metadata, not a resume pointer: it is
// populated only after the daemon confirms a disposable worktree is gone.
func (s *TaskService) CompleteTask(ctx context.Context, taskID pgtype.UUID, result []byte, sessionID, workDir, branchName string, sessionRolloutMissing bool, retiredSessionID, durableWorkDir string) (*db.AgentTaskQueue, error) {
	var task db.AgentTaskQueue
	// chatAssistantMsg is the single assistant outcome row written for a chat
	// task inside the completion transaction below. It is broadcast (chat:done)
	// only after the transaction commits.
	var chatAssistantMsg *db.ChatMessage
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if err := lockChatSessionForTaskWrite(ctx, qtx, taskID); err != nil {
			return err
		}
		t, err := qtx.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{
			ID:                    taskID,
			Result:                result,
			SessionID:             pgtype.Text{String: sessionID, Valid: sessionID != ""},
			WorkDir:               pgtype.Text{String: workDir, Valid: workDir != ""},
			DurableWorkDir:        pgtype.Text{String: durableWorkDir, Valid: durableWorkDir != ""},
			BranchName:            pgtype.Text{String: branchName, Valid: branchName != ""},
			SessionRolloutMissing: sessionRolloutMissing,
			RetiredSessionID:      pgtype.Text{String: retiredSessionID, Valid: retiredSessionID != ""},
		})
		if err != nil {
			return err
		}
		task = t

		if t.ChatSessionID.Valid {
			// Pin the chat_session's runtime_id alongside the session_id so the
			// next claim can apply the runtime-guard. Both fields move together:
			// when there's no session_id to record, leave runtime_id untouched
			// (NULL → COALESCE keeps the existing value).
			var sessionRuntimeID pgtype.UUID
			if sessionID != "" {
				sessionRuntimeID = t.RuntimeID
			}
			// COALESCE in SQL guarantees empty inputs don't wipe the
			// existing resume pointer; we still surface DB errors.
			if err := qtx.UpdateChatSessionSession(ctx, db.UpdateChatSessionSessionParams{
				ID:        t.ChatSessionID,
				SessionID: pgtype.Text{String: sessionID, Valid: sessionID != ""},
				WorkDir:   pgtype.Text{String: workDir, Valid: workDir != ""},
				RuntimeID: sessionRuntimeID,
			}); err != nil {
				return fmt.Errorf("update chat session resume pointer: %w", err)
			}
			// A turn that recovered by abandoning its session still has to
			// retire it here. The COALESCE above cannot: a fresh retry that
			// succeeds without emitting a new id passes sessionID == "", which
			// preserves the pointer — and the pointer is the poisoned session
			// (GH #6066). Runs after the update so a real new id wins first and
			// the match guard then finds nothing to clear.
			if retiredSessionID != "" {
				if err := qtx.ClearChatSessionSessionIfMatches(ctx, db.ClearChatSessionSessionIfMatchesParams{
					ID:        t.ChatSessionID,
					SessionID: pgtype.Text{String: retiredSessionID, Valid: true},
					RuntimeID: t.RuntimeID,
				}); err != nil {
					return fmt.Errorf("clear retired chat session resume pointer: %w", err)
				}
			}

			// Write the assistant outcome in the SAME transaction as the status
			// flip and resume-pointer update (MUL-4351). For a task-owned direct
			// task this is exactly one row (message or no_response); for a
			// legacy/channel task an empty output writes no row (see
			// writeChatCompletionOutcome). Failing here rolls the whole completion
			// back so the daemon retries the terminal callback, and the status CAS
			// above guarantees a replay can't write a second row.
			msg, err := s.writeChatCompletionOutcome(ctx, qtx, t, result)
			if err != nil {
				return fmt.Errorf("write chat assistant outcome: %w", err)
			}
			chatAssistantMsg = msg
		}
		return nil
	}); err != nil {
		// When parallel agents race, a task may already be completed,
		// cancelled, or failed by the time this call runs. The UPDATE
		// … WHERE status = 'running' returns no rows in that case.
		// Treat it as an idempotent success — same pattern as CancelTask.
		if existing, lookupErr := s.Queries.GetAgentTask(ctx, taskID); lookupErr == nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.Info("complete task: already finalized",
					"task_id", util.UUIDToString(taskID),
					"current_status", existing.Status,
					"agent_id", util.UUIDToString(existing.AgentID),
				)
				return &existing, nil
			}
			slog.Warn("complete task failed",
				"task_id", util.UUIDToString(taskID),
				"current_status", existing.Status,
				"issue_id", util.UUIDToString(existing.IssueID),
				"chat_session_id", util.UUIDToString(existing.ChatSessionID),
				"agent_id", util.UUIDToString(existing.AgentID),
				"error", err,
			)
		} else {
			slog.Warn("complete task failed: task not found",
				"task_id", util.UUIDToString(taskID),
				"lookup_error", lookupErr,
			)
		}
		return nil, fmt.Errorf("complete task: %w", err)
	}

	slog.Info("task completed", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(task.IssueID))
	s.captureTaskCompleted(ctx, task)

	// Invariant: every completed issue task must have at least one agent
	// comment on the issue, so the user always sees something when a run
	// ends. If the agent posted a comment during execution (result, progress
	// ping, or CLI reply), HasAgentCommentedSince returns true and we skip.
	// Otherwise, synthesize one from the final output. For comment-triggered
	// tasks, TriggerCommentID threads the fallback under the original comment;
	// for assignment-triggered tasks it is NULL and the fallback is top-level.
	// Chat tasks have no IssueID and are handled separately below.
	if task.IssueID.Valid {
		suppressNoActionComment, err := HasSquadLeaderNoActionEvaluationForTask(ctx, s.Queries, task)
		if err != nil {
			slog.Warn("checking squad leader no_action evaluation failed",
				"task_id", util.UUIDToString(task.ID),
				"issue_id", util.UUIDToString(task.IssueID),
				"agent_id", util.UUIDToString(task.AgentID),
				"error", err,
			)
		}
		agentCommented, _ := s.Queries.HasAgentCommentedSince(ctx, db.HasAgentCommentedSinceParams{
			IssueID:  task.IssueID,
			AuthorID: task.AgentID,
			Since:    task.StartedAt,
		})
		if !suppressNoActionComment && !agentCommented {
			var payload protocol.TaskCompletedPayload
			if err := json.Unmarshal(result, &payload); err == nil {
				if payload.Output != "" {
					// Match the CLI's --content / --description behavior: agents that
					// emit literal `\n` 4-char sequences (Python/JSON-style) get them
					// decoded into real newlines before the comment hits the DB. See
					// util.UnescapeBackslashEscapes for the exact contract.
					body := util.UnescapeBackslashEscapes(payload.Output)
					if task.TriggerCommentID.Valid && isTrivialDoneOutput(body) {
						slog.Warn("suppressing trivial comment-trigger fallback output",
							"task_id", util.UUIDToString(task.ID),
							"issue_id", util.UUIDToString(task.IssueID),
							"agent_id", util.UUIDToString(task.AgentID),
						)
					} else {
						// Redact first, then bound: a runaway raw-stream Output (GH #5455)
						// must never reach the issue thread, even as a clipped excerpt.
						content := truncateFallbackCommentBody(redact.Text(body), maxSynthesizedFallbackCommentRunes)
						s.createAgentComment(ctx, task.IssueID, task.AgentID, content, "comment", task.TriggerCommentID, task.ID)
					}
				}
			}
		}
	}

	// Quick-create tasks: locate the issue the agent just created and push
	// an inbox confirmation to the requester. The agent has no issue / chat
	// link, so the regular completion paths above don't apply. We find the
	// new issue by querying for the most recent issue this agent created in
	// the requester's workspace since the task started — more robust than
	// parsing the agent's stdout for an identifier.
	if qc, ok := s.parseQuickCreateContext(task); ok {
		s.notifyQuickCreateCompleted(ctx, task, qc, result)
	}

	// For chat tasks, broadcast chat:done AFTER commit. The single assistant
	// outcome row (message or no_response) and the resume pointer were already
	// persisted inside the transaction above. Unread is derived from the read
	// cursor (chat_session.last_read_at) vs the assistant messages after it — a
	// no_response row has role='assistant' and a fresh created_at, so it counts
	// as unread just like a text reply, no per-reply stamping needed.
	if task.ChatSessionID.Valid {
		// The assistant outcome row (message / no_response) and any attachment
		// binding were written inside the completion transaction above by
		// writeChatCompletionOutcome. Broadcast chat:done AFTER commit.
		//
		// The quick-actions decision is made here, before the broadcast, so the
		// pending flag and the pass that resolves it can never disagree: a
		// client only shows the skeleton placeholder when generation is
		// actually about to run.
		suggest := s.chatQuickActionsEligible(ctx, task, chatAssistantMsg)
		s.broadcastChatDone(ctx, task, chatAssistantMsg, suggest)
		if suggest {
			// Detached: the reply is already delivered and the user's next turn
			// must never wait on suggestions.
			s.GenerateChatQuickActionsAsync(task, ChatQuickActionsAutomatic)
		}
	}

	// Reconcile agent status
	s.ReconcileAgentStatus(ctx, task.AgentID)

	// Broadcast
	s.broadcastTaskEvent(ctx, protocol.EventTaskCompleted, task)

	return &task, nil
}

// chatNoResponseFallback is the non-empty English body stored on a no_response
// assistant row. New clients render a localized "no text reply this turn"
// message keyed on message_kind='no_response'; older clients that ignore
// message_kind still show this text instead of an empty bubble (MUL-4351).
const chatNoResponseFallback = "The agent finished this turn without a text reply."

// writeChatCompletionOutcome writes the assistant chat_message outcome for a
// completed chat task inside the caller's completion transaction, returning the
// row (nil when none is written).
//
// Direct (web/mobile) tasks get the explicit single-outcome contract: a
// non-empty final output becomes an ordinary assistant message, and an
// empty/whitespace output becomes a visible no_response outcome carrying a
// non-empty English fallback body. It never auto-retries: an empty output is a
// legitimate terminal result (a tool-only turn) and re-running it would repeat
// side effects already performed.
//
// Channel (Slack/Lark) and legacy tasks keep the prior behavior: a non-empty
// output writes an ordinary assistant message, but an EMPTY output writes NO
// row, so chat:done carries empty content and the channel outbound silently
// drops it (MUL-4351 review): the no_response fallback body must never be
// pushed to an external channel. Channel tasks now own a sealed input batch
// (chat_input_task_id set) just like direct tasks, so the discriminator is the
// immutable channel_ingested stamp on the owned batch — keyed by the batch
// owner id so an auto-retry clone (which inherits chat_input_task_id) reaches
// the same verdict as its parent — while a NULL owner marks a legacy task.
func (s *TaskService) writeChatCompletionOutcome(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue, result []byte) (*db.ChatMessage, error) {
	// result is the daemon request re-marshalled by the handler, so it is always
	// valid JSON; an empty Output is the only case this branch cares about.
	var payload protocol.TaskCompletedPayload
	_ = json.Unmarshal(result, &payload)
	// Same unescape as the issue-comment path: literal `\n` from agent stdout
	// becomes a real newline so the chat panel renders paragraph breaks.
	body := util.UnescapeBackslashEscapes(payload.Output)
	// Strip any in-band quick-actions footer from EVERY chat completion — the
	// reserved syntax must never reach a stored transcript. This includes the
	// agent-initiated intro turn (chat_input_task_id NULL), which previously
	// fell outside the strip gate and leaked the raw footer into its content;
	// channel outputs never carry the syntax, so the split is a no-op there.
	//
	// The parsed footer is DISCARDED: suggestions come from the server-side pass
	// now (SupplementChatQuickActions). Honouring a footer here would pin a
	// pre-upgrade session to the retired, lower-quality in-band suggestions and
	// suppress the pass that would have replaced them.
	body, _ = splitChatQuickActions(body)
	isEmpty := strings.TrimSpace(body) == ""

	// MUL-4899 completion-boundary observation. Measures whether the delivery
	// contract in the runtime brief is actually landing on the chat surface.
	// Strictly non-blocking: the reply is written either way.
	s.observeChatOutputLocalPath(task, body)

	// Attachments the agent uploaded during this task (tagged with task_id, not
	// yet bound to any owner) are part of this reply. They make an empty-text
	// turn a real image/file response — NOT a no_response — and need a row to
	// hang on. Count + bind run on qtx so message creation and binding are one
	// atomic outcome.
	wsUUID, _ := util.ParseUUID(s.ResolveTaskWorkspaceID(ctx, task))
	var pendingAttachments int64
	if wsUUID.Valid {
		n, err := qtx.CountUnboundChatAttachmentsForTask(ctx, db.CountUnboundChatAttachmentsForTaskParams{
			WorkspaceID: wsUUID,
			TaskID:      task.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("count chat attachments: %w", err)
		}
		pendingAttachments = n
	}

	// Channel/legacy empty completion with nothing to show: emit no assistant
	// row, only an empty chat:done for typing/lifecycle. Keeps the Slack/Lark
	// silent-drop path — the outbound patcher forwards any non-empty content
	// verbatim, so the fallback body must never be written for a channel task.
	// Attachments still force a row — the agent produced a deliverable the
	// user must see.
	if isEmpty && pendingAttachments == 0 {
		if !task.ChatInputTaskID.Valid {
			return nil, nil // legacy task
		}
		channelIngested, err := qtx.TaskHasChannelIngestedMessages(ctx, task.ChatInputTaskID)
		if err != nil {
			return nil, fmt.Errorf("check chat completion channel provenance: %w", err)
		}
		if channelIngested {
			return nil, nil // channel task
		}
	}

	params := db.CreateChatMessageParams{
		ID:            dbid.NewV7(),
		ChatSessionID: task.ChatSessionID,
		Role:          "assistant",
		TaskID:        task.ID,
		ElapsedMs:     computeChatElapsedMs(task),
	}
	switch {
	case !isEmpty:
		params.Content = redact.Text(body)
		// message_kind left NULL → COALESCE defaults to 'message'.
		//
		// The one exception is now a deploy-window case. The server writes the
		// opening directly (MUL-5827), so no new task ever produces one — but a
		// kickoff task enqueued by the previous server can still be claimed by
		// this one, and its reply IS that member's opening. Leaving it a plain
		// 'message' would permanently cost that session its starter cards.
		//
		// Gated on "the batch is a kickoff and nothing else", which is precisely
		// the old shape: the new kickoff rides in alongside a real member
		// message, and stamping that turn would render the cards a second time
		// under a reply that is not an opening.
		//
		// Keyed on chatInputOwnerID, not task.ID: an auto-retry clone gets a
		// fresh id while inheriting the root's chat_input_task_id (MUL-4351),
		// and the kickoff user row stays bound to the root.
		openingOnly, err := qtx.TaskInputIsOnboardingKickoffOnly(ctx, chatInputOwnerID(task))
		if err != nil {
			return nil, fmt.Errorf("check onboarding kickoff input: %w", err)
		}
		if openingOnly.Bool {
			params.MessageKind = pgtype.Text{String: protocol.ChatMessageKindOnboardingOpening, Valid: true}
		}
	case pendingAttachments > 0:
		// Image/file-only reply: a real 'message' outcome with empty text — the
		// attachment cards ARE the response, so it must not read as no_response.
		params.Content = ""
	default:
		// Task-owned direct task, empty output, no attachments: explicit,
		// visible no_response outcome.
		params.Content = chatNoResponseFallback
		params.MessageKind = pgtype.Text{String: protocol.ChatMessageKindNoResponse, Valid: true}
	}
	row, err := createAssistantChatMessage(ctx, qtx, params)
	if err != nil {
		return nil, err
	}

	// Bind the task's still-unclaimed attachments to the reply we just wrote.
	if pendingAttachments > 0 && wsUUID.Valid {
		bound, err := qtx.BindChatAttachmentsToMessage(ctx, db.BindChatAttachmentsToMessageParams{
			ChatMessageID: row.ID,
			WorkspaceID:   wsUUID,
			TaskID:        task.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("bind chat attachments: %w", err)
		}
		if len(bound) > 0 {
			slog.Info("bound chat attachments to assistant reply",
				"task_id", util.UUIDToString(task.ID),
				"message_id", util.UUIDToString(row.ID),
				"count", len(bound),
			)
		}
	}
	return &row, nil
}

// observeChatOutputLocalPath records a metric when a chat reply references a
// runtime-local path (MUL-4899). Observation only — it never mutates the reply,
// never fails the completion, and makes no claim to have fixed anything.
//
// Two hard constraints shape it:
//
//  1. Lexical only. The path lives on the daemon's machine, so the server cannot
//     os.Stat it the way the CLI lint can. That leaves two signals it can be
//     confident about without guessing: a `file://` URL, and the task's own
//     recorded work_dir as a prefix. Anything subtler would be a guess, and a
//     guess is not worth a false signal on a dashboard.
//  2. No path, no body text, and no fragment of either may reach the metric or
//     the log — only the classification and the task id.
func (s *TaskService) observeChatOutputLocalPath(task db.AgentTaskQueue, body string) {
	if s.Metrics == nil || strings.TrimSpace(body) == "" {
		return
	}
	kind := ""
	switch {
	case strings.Contains(strings.ToLower(body), "file://"):
		kind = "file_url"
	case task.WorkDir.Valid && task.WorkDir.String != "" && strings.Contains(body, task.WorkDir.String):
		kind = "workdir_path"
	default:
		return
	}
	s.Metrics.RecordChatOutputLocalPath(kind)
	slog.Warn("chat reply references a runtime-local path",
		"task_id", util.UUIDToString(task.ID),
		"kind", kind,
	)
}

// FailTask marks a task as failed.
// Issue status is NOT changed here — the agent manages it via the CLI.
//
// sessionID/workDir are optional: when the agent established a real session
// before failing (e.g. crashed mid-conversation, was cancelled, or hit a
// tool error), the daemon should pass them so we can preserve the resume
// pointer on both the task row and the chat_session — otherwise the next
// chat turn would silently start a brand-new session and lose memory.
// durableWorkDir is persisted on the task only; it never replaces the actual
// workDir used for session resumption.
//
// failureReason is a coarse classifier consumed by the auto-retry path.
// Pass "" when unknown — the server runs the raw error text through
// taskfailure.Classify so the persisted failure_reason still lands in
// the canonical refined taxonomy rather than the legacy "agent_error"
// coarse bucket. Daemon callers that already produced a refined reason
// (via classifyPoisonedError, the timeout / runtime classifier, etc.)
// will have their value preserved untouched.
func (s *TaskService) FailTask(ctx context.Context, taskID pgtype.UUID, errMsg, sessionID, workDir, branchName, failureReason string, sessionRolloutMissing bool, retiredSessionID, durableWorkDir string) (*db.AgentTaskQueue, error) {
	// Strip bytes PostgreSQL cannot store before anything else reads errMsg, so
	// the classifier, the transaction and every downstream consumer see the one
	// text we will actually persist (GH #7098). Kept at the service boundary
	// rather than only in the /fail handler because failClaimedTaskBeforeLaunch
	// reaches FailTask without going through request decoding.
	errMsg = util.SanitizeTextForPostgres(errMsg)

	// MUL-2946: synthesise a refined reason from the error text whenever the
	// caller didn't supply one. This is the last write-path guard against
	// "agent_error" coarse rows ending up in agent_task_queue.failure_reason
	// — every other path either provides a classified reason directly
	// (sweepers writing 'queued_expired' / 'runtime_offline' / 'timeout'
	// / 'runtime_recovery' via SQL) or runs the daemon's classifyPoisonedError
	// + taskfailure.Classify chain.
	if failureReason == "" {
		failureReason = taskfailure.Classify(errMsg).String()
	}
	// MUL-5370: daemons upgrade on their own cadence, so a fix that depends on
	// a new daemon-side label only reaches hosts that happened to update. An
	// older daemon reports a *non-empty* catchall for a failed skill-bundle
	// download, which the branch above deliberately leaves alone — without this
	// the retry and the actionable copy would both skip every un-upgraded host.
	// Runs after the empty-reason branch so a legacy reason synthesised there
	// is normalised too, and before the retry pre-compute below so the upgraded
	// reason is what decides retry eligibility.
	failureReason = taskfailure.NormalizeDaemonReason(failureReason, errMsg).String()

	// Pre-compute the auto-retry so the retry child can be created inside the
	// SAME transaction as the fail (MUL-4351). Doing it atomically closes the
	// window between the fail committing and the retry appearing during which a
	// newer chat task could claim the now-idle session and jump ahead of the
	// retry. The overlay build can do network I/O (Composio), so we resolve it
	// here — before the transaction — and only for retryable failures, so the
	// common agent_error path skips this work entirely.
	var (
		wantRetry        bool
		retryOverlay     runtimeMCPOverlayData
		retryFireAt      pgtype.Timestamptz
		retryMaxAttempts pgtype.Int4
	)
	if retryableReasons[failureReason] {
		if parent, perr := s.Queries.GetAgentTask(ctx, taskID); perr != nil {
			slog.Warn("fail task auto-retry: load parent failed",
				"task_id", util.UUIDToString(taskID), "error", perr)
		} else if retryEligible(failureReason, parent) {
			wantRetry = true
			// Persist the reason-aware effective budget into the child so the
			// retry chain self-describes (e.g. provider_network → max_attempts=3),
			// rather than leaking a contradictory attempt=N/max_attempts=2 row.
			retryMaxAttempts = pgtype.Int4{Int32: retryAttemptCeiling(failureReason, parent.MaxAttempts), Valid: true}
			// Defer this attempt when the reason's schedule calls for a backoff
			// (provider_network's final attempt waits ~5s); a zero delay leaves
			// fire_at NULL so the child is created immediately-claimable.
			if delay := retryDelayForAttempt(failureReason, parent.Attempt); delay > 0 {
				retryFireAt = pgtype.Timestamptz{Time: time.Now().Add(delay), Valid: true}
			}
			if agent, aerr := s.Queries.GetAgent(ctx, parent.AgentID); aerr != nil {
				// Best-effort: a missing overlay is not retry-fatal — the child
				// simply runs without the Composio overlay.
				slog.Warn("fail task auto-retry: load agent for overlay failed",
					"task_id", util.UUIDToString(taskID),
					"agent_id", util.UUIDToString(parent.AgentID), "error", aerr)
			} else {
				retryOverlay = s.buildRuntimeMCPOverlay(ctx, parent.OriginatorUserID, agent)
			}
		}
	}

	var task db.AgentTaskQueue
	var retried *db.AgentTaskQueue
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if err := lockChatSessionForTaskWrite(ctx, qtx, taskID); err != nil {
			return err
		}
		t, err := qtx.FailAgentTask(ctx, db.FailAgentTaskParams{
			ID:             taskID,
			Error:          pgtype.Text{String: errMsg, Valid: true},
			FailureReason:  pgtype.Text{String: failureReason, Valid: failureReason != ""},
			SessionID:      pgtype.Text{String: sessionID, Valid: sessionID != ""},
			WorkDir:        pgtype.Text{String: workDir, Valid: workDir != ""},
			DurableWorkDir: pgtype.Text{String: durableWorkDir, Valid: durableWorkDir != ""},
			// A failed run can still have produced a branch: worktree mode
			// commits whatever the agent left before tearing the worktree down,
			// precisely so partial work survives. Dropping the name here would
			// leave that commit with no pointer to it.
			BranchName:            pgtype.Text{String: branchName, Valid: branchName != ""},
			SessionRolloutMissing: sessionRolloutMissing,
			RetiredSessionID:      pgtype.Text{String: retiredSessionID, Valid: retiredSessionID != ""},
		})
		if err != nil {
			return err
		}
		task = t

		// Keep resume-unsafe sessions on the task row for observability, but
		// do not promote them to the chat-level resume pointer.
		//
		// Declining to overwrite is not enough on its own: the claim handler
		// reads chat_session.session_id BEFORE consulting
		// GetLastChatTaskSession, so a pointer already holding the dead
		// session bypasses every filter that query applies and the next turn
		// resumes it (GH #6066). Clear it in this same transaction, matched on
		// the exact session and runtime so a concurrent turn's newer pointer
		// is left alone. ResumeUnsafeFailure (not the reason alone) so an
		// un-upgraded daemon's agent_error.unknown row is caught by the error
		// text too.
		if t.ChatSessionID.Valid && ResumeUnsafeFailure(failureReason, errMsg) {
			deadSession := sessionID
			if deadSession == "" {
				deadSession = t.SessionID.String
			}
			if deadSession != "" {
				if err := qtx.ClearChatSessionSessionIfMatches(ctx, db.ClearChatSessionSessionIfMatchesParams{
					ID:        t.ChatSessionID,
					SessionID: pgtype.Text{String: deadSession, Valid: true},
					RuntimeID: t.RuntimeID,
				}); err != nil {
					return fmt.Errorf("clear poisoned chat session resume pointer: %w", err)
				}
			}
		}
		// A session the run explicitly retired must go too, whatever the
		// terminal status: the fresh-session retry that replaced it may well
		// have succeeded, and the pointer would otherwise still name the
		// transcript it retried away from.
		if t.ChatSessionID.Valid && retiredSessionID != "" {
			if err := qtx.ClearChatSessionSessionIfMatches(ctx, db.ClearChatSessionSessionIfMatchesParams{
				ID:        t.ChatSessionID,
				SessionID: pgtype.Text{String: retiredSessionID, Valid: true},
				RuntimeID: t.RuntimeID,
			}); err != nil {
				return fmt.Errorf("clear retired chat session resume pointer: %w", err)
			}
		}

		// ResumeUnsafeFailure, not resumeUnsafeFailureReason: the reason-only
		// check passes an un-upgraded daemon's agent_error.unknown row, which
		// would re-pin the very session the clear above just removed.
		if t.ChatSessionID.Valid && !ResumeUnsafeFailure(failureReason, errMsg) {
			// Pin the chat_session's runtime_id alongside the session_id so the
			// next claim can apply the runtime-guard. Both fields move together:
			// when there's no session_id to record, leave runtime_id untouched
			// (NULL → COALESCE keeps the existing value).
			var sessionRuntimeID pgtype.UUID
			if sessionID != "" {
				sessionRuntimeID = t.RuntimeID
			}
			if err := qtx.UpdateChatSessionSession(ctx, db.UpdateChatSessionSessionParams{
				ID:        t.ChatSessionID,
				SessionID: pgtype.Text{String: sessionID, Valid: sessionID != ""},
				WorkDir:   pgtype.Text{String: workDir, Valid: workDir != ""},
				RuntimeID: sessionRuntimeID,
			}); err != nil {
				return fmt.Errorf("update chat session resume pointer: %w", err)
			}
		}

		// Create the retry child atomically with the fail. CreateRetryTask reads
		// the just-failed parent row (same tx), so it inherits chat_input_task_id
		// and the bumped chat-retry priority; broadcast/notify happen after commit.
		// This check is an optimisation, NOT the safety mechanism. It is a plain
		// count at READ COMMITTED and takes no lock, so a rerun can always commit
		// between it and the insert below; it only saves the work when a
		// successor is already visible and gives the skip a readable log line.
		// Correctness under that race belongs to CreateRetryTask's
		// ON CONFLICT DO NOTHING, which yields the slot instead of raising and so
		// can never abort this transaction — which also carries the parent's
		// failed status.
		createRetry := wantRetry
		if createRetry {
			successor, herr := hasRunnableSuccessor(ctx, qtx, t)
			if herr != nil {
				return fmt.Errorf("check runnable successor: %w", herr)
			}
			if successor {
				slog.Info("fail task auto-retry skipped: a successor is already pending",
					"task_id", util.UUIDToString(taskID),
					"issue_id", util.UUIDToString(t.IssueID),
					"agent_id", util.UUIDToString(t.AgentID),
				)
				createRetry = false
			}
		}
		if createRetry {
			child, cerr := qtx.CreateRetryTask(ctx, db.CreateRetryTaskParams{
				NewTaskID:            dbid.NewV7(),
				ID:                   taskID,
				FireAt:               retryFireAt,
				MaxAttempts:          retryMaxAttempts,
				RuntimeMcpOverlay:    retryOverlay.Overlay,
				RuntimeConnectedApps: retryOverlay.ConnectedApps,
			})
			switch {
			case cerr == nil:
				transferErr := transferPendingSourceContextToRetry(ctx, qtx, t, child)
				if errors.Is(transferErr, pgx.ErrNoRows) {
					deleted, deleteErr := qtx.DeleteUnstartedQuickCreateRetryTask(ctx, child.ID)
					if deleteErr != nil || deleted != 1 {
						return fmt.Errorf("discard source context retry without attach authority: changed=%d: %w", deleted, deleteErr)
					}
					slog.Info("fail task auto-retry skipped: source context already transferred or attached",
						"task_id", util.UUIDToString(taskID),
						"source_context_retry_id", util.UUIDToString(child.ID),
					)
				} else if transferErr != nil {
					return fmt.Errorf("transfer pending source context to retry: %w", transferErr)
				} else {
					if err := qtx.CopyChannelTaskDelivery(ctx, db.CopyChannelTaskDeliveryParams{
						ChildTaskID: child.ID, ParentTaskID: taskID,
					}); err != nil {
						return fmt.Errorf("copy retry channel delivery: %w", err)
					}
					retried = &child
				}
			case errors.Is(cerr, pgx.ErrNoRows):
				// The statement wrote nothing: either the owning workspace was
				// torn down mid-flight, or a rerun took the pending slot after
				// the unlocked check above (ON CONFLICT DO NOTHING). Neither is
				// a reason to abort — this transaction still owns the parent's
				// failed status, and losing that would leave the task stuck in
				// 'running'. Record the failure and move on without a retry.
				slog.Info("fail task auto-retry not created: no row written",
					"task_id", util.UUIDToString(taskID),
					"issue_id", util.UUIDToString(t.IssueID),
					"agent_id", util.UUIDToString(t.AgentID),
				)
			default:
				return fmt.Errorf("create retry task: %w", cerr)
			}
		}

		// A terminal non-retried chat failure is a visible assistant outcome.
		// Persist it while the session lock and failure transaction are still
		// held, then reanchor the next direct head. Otherwise the successor could
		// be claimed between the status flip and this row, placing its user input
		// before the failure it follows.
		if t.ChatSessionID.Valid && retried == nil {
			// This turn is dead, so anything it owned has to move on. An adopted
			// onboarding kickoff would otherwise stay bound to a task that will
			// never run again: the next turn would reach Mika with no onboarding
			// context and no record that she had already greeted the member, and
			// she would introduce herself a second time (MUL-5827). The query
			// hands it to the session's next queued turn — including one the
			// member queued while THIS turn was still running, which adoption at
			// send time could not have caught.
			//
			// Gated on retried == nil on purpose. A retry child inherits the
			// root's chat_input_task_id, and the kickoff stays bound to that
			// root, so the retry still reads it — releasing here would strip
			// the context off a turn that is about to run.
			if err := qtx.ReleaseOnboardingKickoffFromTask(ctx, chatInputOwnerID(t)); err != nil {
				return fmt.Errorf("release onboarding kickoff: %w", err)
			}
			if _, err := createAssistantChatMessage(ctx, qtx, db.CreateChatMessageParams{
				ID:            dbid.NewV7(),
				ChatSessionID: t.ChatSessionID,
				Role:          "assistant",
				Content:       redact.Text(errMsg),
				TaskID:        t.ID,
				FailureReason: pgtype.Text{String: failureReason, Valid: failureReason != ""},
				ElapsedMs:     computeChatElapsedMs(t),
			}); err != nil {
				return fmt.Errorf("write chat failure outcome: %w", err)
			}
		}
		return nil
	}); err != nil {
		if existing, lookupErr := s.Queries.GetAgentTask(ctx, taskID); lookupErr == nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.Info("fail task: already finalized",
					"task_id", util.UUIDToString(taskID),
					"current_status", existing.Status,
					"agent_id", util.UUIDToString(existing.AgentID),
				)
				return &existing, nil
			}
			slog.Warn("fail task failed",
				"task_id", util.UUIDToString(taskID),
				"current_status", existing.Status,
				"issue_id", util.UUIDToString(existing.IssueID),
				"chat_session_id", util.UUIDToString(existing.ChatSessionID),
				"agent_id", util.UUIDToString(existing.AgentID),
				"error", err,
			)
		} else {
			slog.Warn("fail task failed: task not found",
				"task_id", util.UUIDToString(taskID),
				"lookup_error", lookupErr,
			)
		}
		return nil, fmt.Errorf("fail task: %w", err)
	}

	slog.Warn("task failed", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(task.IssueID), "error", errMsg, "failure_reason", failureReason)
	s.captureTaskFailed(ctx, task)

	// The auto-retry child (if any) was created inside the transaction above so
	// no newer chat task could jump ahead of it. Surface it now: broadcast
	// queued first, then notify the daemon — see EnqueueTaskForIssue for the
	// ordering rationale. A deferred child (backoff armed via fire_at) is NOT
	// queued yet: PromoteDueDeferredTasksForRuntime emits its queued event and
	// daemon wakeup when fire_at arrives, so announcing it here would be wrong.
	if retried != nil {
		slog.Info("task auto-retry enqueued",
			"parent_task_id", util.UUIDToString(task.ID),
			"child_task_id", util.UUIDToString(retried.ID),
			"reason", failureReason,
			"attempt", retried.Attempt,
			"max_attempts", retried.MaxAttempts,
			"status", retried.Status,
		)
		if retried.Status == "queued" {
			s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, *retried)
			s.NotifyTaskEnqueued(ctx, *retried)
		}
	}

	// A delegated task that has reached a terminal failure must hand control
	// back to the task that delegated it. This runs only after the existing
	// retry decision: retryable failures keep their current policy and remain
	// silent while a child attempt is pending. The recovery signal is a
	// platform-authored comment on the source task's issue, routed explicitly to
	// that task's agent; recoverDelegatedTaskFailure coalesces it with an
	// existing coordinator run and deduplicates by the failed task id.
	if retried == nil {
		_, recoveryErr := s.recoverDelegatedTaskFailure(ctx, task)
		if recoveryErr != nil {
			slog.Warn("delegated task failure recovery failed",
				"task_id", util.UUIDToString(task.ID),
				"delegated_from_task_id", util.UUIDToString(task.DelegatedFromTaskID),
				"error", recoveryErr,
			)
		}
	}

	// Skip the per-failure system comment when we'll immediately retry —
	// the new task will surface its own status to the user, and we don't
	// want to spam the issue with "task timed out" messages on every
	// daemon hiccup. Delegated failures keep this existing failed-issue comment
	// in addition to the coordinator recovery signal, preserving visibility on
	// both sides of a cross-issue handoff.
	if errMsg != "" && task.IssueID.Valid && retried == nil {
		s.createAgentComment(ctx, task.IssueID, task.AgentID, redact.Text(errMsg), "system", task.TriggerCommentID, task.ID)
	}

	// Quick-create tasks: push a failure inbox notification to the
	// requester so they can either retry or fall back to the advanced form
	// without losing their original prompt. Skipped when an auto-retry is
	// pending — the new attempt will write its own outcome.
	if retried == nil {
		if qc, ok := s.parseQuickCreateContext(task); ok {
			attached, attachedErr := s.sourceContextAttachedByTask(ctx, task, qc)
			switch {
			case attachedErr != nil:
				slog.Error("quick-create failure: source context outcome lookup failed",
					"task_id", util.UUIDToString(task.ID), "error", attachedErr)
				s.notifyQuickCreateUnconfirmed(ctx, task, qc)
			case attached:
				// The CLI create committed before the runtime reported its own
				// failure. The attached context is authoritative proof that the
				// target exists, so reconcile the normal success inbox rather than
				// inviting a duplicate retry from a misleading failure row.
				s.notifyQuickCreateCompleted(ctx, task, qc, nil)
			default:
				s.notifyQuickCreateFailed(ctx, task, qc, errMsg)
			}
		}
	}
	// Reconcile agent status
	s.ReconcileAgentStatus(ctx, task.AgentID)

	// Broadcast. Channel subscribers need the same redacted failure text that
	// was persisted in the chat transcript. A retry-pending attempt stays silent
	// because its child reports the eventual terminal outcome.
	s.broadcastTaskFailedEvent(ctx, task, errMsg, failureReason, retried != nil)

	return &task, nil
}

// retryableReasons enumerates failure reasons that the auto-retry path is
// allowed to act on. Agent-side errors (compile failures, model rejections,
// etc.) are intentionally excluded — those are real problems that the user
// should see, not infrastructure flakiness.
//
// The one agent_error.* exception is provider_network: a mid-stream provider
// disconnect (e.g. Claude Code's "API Error: Connection closed mid-response")
// is transient infrastructure flakiness, not an agent decision. Unattended
// issue runs otherwise terminate on it, while interactive chat only survives
// because the CLI's own in-process retry happens to recover first — so we make
// the platform retry it directly (MUL-4910). It is resume-safe (not in
// resumeUnsafeFailureReason), so the retry child inherits the session and
// continues the truncated conversation rather than restarting from scratch.
// skill_bundle_unavailable is retryable for the same reason: the agent process
// never started, so there is nothing to be idempotent about, and every bundle
// that did download is already cached on disk — a retry resumes from there
// instead of re-fetching the whole set (MUL-5370).
var retryableReasons = map[string]bool{
	string(taskfailure.ReasonRuntimeOffline):         true,
	string(taskfailure.ReasonRuntimeRecovery):        true,
	string(taskfailure.ReasonTimeout):                true,
	"codex_semantic_inactivity":                      true,
	string(taskfailure.ReasonAgentProviderNetwork):   true,
	string(taskfailure.ReasonSkillBundleUnavailable): true,
}

// runtime_offline retries start deferred, not queued: their positive fire_at
// routes them through health-gated promotion after a fresh runtime heartbeat
// returns. The queue sweeper also exempts this retry lineage, covering the race
// where a runtime disconnects again after promotion but before claim. This
// delay is only a state marker, not the reconnect policy; the heartbeat gate is
// authoritative.
//
// Transient provider stream cuts (provider_network) get a bespoke three-tier
// schedule (MUL-4910): first run + immediate retry + one retry deferred ~5s.
// A blip that survives the immediate retry gets a short cooldown before the
// final attempt instead of firing back-to-back. Every other retryable reason
// keeps the task's generic max_attempts ceiling and retries immediately.
const (
	runtimeOfflineRetryDeferral   = time.Second
	providerNetworkMaxAttempts    = 3
	providerNetworkFinalRetryWait = 5 * time.Second
)

// retryAttemptCeiling reports how many attempts the auto-retry path allows for
// a failure reason. It only ever WIDENS the task's generic max_attempts, and
// only for reasons with a bespoke schedule; everything else keeps the column's
// value (default 2 = first run + one retry).
//
// max_attempts <= 1 explicitly disables auto-retry (055_task_lease_and_retry.up
// .sql: "1 disables retry"), so it is never overridden — a disabled task must
// not be revived by a raised ceiling. Callers persist this value into the retry
// child (CreateRetryTask's max_attempts) so the row stays self-consistent:
// provider_network's chain records attempt=3, max_attempts=3, not a
// contradictory attempt=3, max_attempts=2 (MUL-4910).
func retryAttemptCeiling(reason string, taskMaxAttempts int32) int32 {
	if taskMaxAttempts <= 1 {
		return taskMaxAttempts
	}
	if reason == string(taskfailure.ReasonAgentProviderNetwork) && taskMaxAttempts < providerNetworkMaxAttempts {
		return providerNetworkMaxAttempts
	}
	return taskMaxAttempts
}

// retryDelayForAttempt reports how long to defer the NEXT attempt after a
// failure at failedAttempt. runtime_offline always gets a positive fire_at so
// it waits for the health-gated promotion path. provider_network's final
// attempt is deferred ~5s; every other retry remains immediate (zero delay →
// the child is created 'queued', claimable at once). Callers pass the returned
// delay to CreateRetryTask via fire_at.
func retryDelayForAttempt(reason string, failedAttempt int32) time.Duration {
	if reason == string(taskfailure.ReasonRuntimeOffline) {
		return runtimeOfflineRetryDeferral
	}
	if reason == string(taskfailure.ReasonAgentProviderNetwork) &&
		failedAttempt >= providerNetworkMaxAttempts-1 {
		return providerNetworkFinalRetryWait
	}
	return 0
}

func resumeUnsafeFailureReason(reason string) bool {
	switch reason {
	// Failures that poison the agent CONVERSATION (not the workdir): resuming
	// the same session would immediately replay the stuck/oversized state.
	// Keep in sync with the GetLastTaskSession / GetLastChatTaskSession resume
	// blacklists. (CreateRetryTask's fresh-session CASE WHEN only needs the
	// subset of these that is also auto-retryable, currently
	// codex_semantic_inactivity.)
	// codex_resume_oversized is the strongest member of this set: a codex
	// rollout only ever grows, so a thread whose resume response already
	// overflowed the reader will overflow on every future attempt too.
	case "iteration_limit", "agent_fallback_message", "api_invalid_request", "codex_semantic_inactivity", "agent_error.context_overflow", "codex_resume_oversized":
		return true
	default:
		return false
	}
}

// ResumeUnsafeFailure reports whether a failed task's agent session must NOT be
// resumed on a retry. It combines the failure_reason poison set
// (resumeUnsafeFailureReason) with the SAME defense-in-depth on raw error text
// that the GetLastTaskSession / GetLastChatTaskSession resume queries apply: an
// Anthropic 400 invalid_request_error means the conversation history itself is
// unprocessable even when failure_reason was mis- or un-classified (legacy
// 'agent_error' rows written before MUL-1921, or deploy-window rows). Callers
// that only have a failure_reason (e.g. at fail time) may pass an empty
// errorText.
//
// This is the shared source of truth for the manual-retry claim path, which
// reads the exact source task instead of GetLastTaskSession and would otherwise
// bypass the error-text guard.
func ResumeUnsafeFailure(failureReason, errorText string) bool {
	if resumeUnsafeFailureReason(failureReason) {
		return true
	}
	lower := strings.ToLower(errorText)
	if strings.Contains(lower, "400") && strings.Contains(lower, "invalid_request_error") {
		return true
	}
	// Provider credential-resolution failures are deterministic on resume: the
	// missing api_key / auth_token / auth header is baked into the session's
	// provider state, so a rerun must start fresh instead of replaying the same
	// auth error on the recorded (agent, issue) session. taskfailure.Classify
	// deliberately leaves this error as agent_error.unknown, so this
	// reason-independent text guard is the load-bearing protection for both new
	// and already persisted rows. Keep it in sync with the GetLastTaskSession /
	// GetLastChatTaskSession resume queries.
	//
	// The phrase itself lives in taskfailure.AuthMethodUnresolved, shared with
	// the daemon's in-turn fresh-session retry gate so the two layers cannot
	// disagree about which errors mean "this session can never be resumed".
	if taskfailure.AuthMethodUnresolved(errorText) {
		return true
	}
	// Same defense-in-depth for the provider-agnostic empty-message shape:
	// a daemon too old to carry classifyPoisonedError's new branch reports
	// agent_error.unknown, and without this the manual-retry path would
	// happily resume the transcript the provider just refused (GH #6066).
	return taskfailure.UnresumableHistory(errorText)
}

// retryEligible reports whether a failed task qualifies for an automatic retry
// attempt: an infrastructure-shaped failure_reason, remaining attempt budget,
// not an autopilot run, and linked to an issue or chat session. Shared by
// FailTask's in-transaction retry and the orphan sweeper's MaybeRetryFailedTask
// so both agree on which failures re-run.
func retryEligible(failureReason string, t db.AgentTaskQueue) bool {
	return retryableReasons[failureReason] &&
		t.Attempt < retryAttemptCeiling(failureReason, t.MaxAttempts) &&
		!t.AutopilotRunID.Valid &&
		(t.IssueID.Valid || t.ChatSessionID.Valid || isSourceContextQuickCreateTask(t))
}

func isSourceContextQuickCreateTask(task db.AgentTaskQueue) bool {
	if len(task.Context) == 0 || task.IssueID.Valid || task.ChatSessionID.Valid || task.AutopilotRunID.Valid {
		return false
	}
	var quickCreate QuickCreateContext
	return json.Unmarshal(task.Context, &quickCreate) == nil &&
		quickCreate.Type == QuickCreateContextType && quickCreate.SourceContextID != ""
}

// hasRunnableSuccessor reports whether another not-yet-started task already
// occupies the single queued/dispatched slot that
// idx_one_pending_task_per_issue_agent_v2 allows per (issue, agent).
//
// Both auto-retry paths consult this before inserting a retry child. A manual
// rerun can now be enqueued BEHIND a still-running task instead of cancelling
// it, so by the time that task fails its slot may already be taken, and the
// retry exists to give the issue a runnable successor that already exists.
//
// This is advisory only: the query takes no lock, so a rerun committing after it
// returns is always possible. CreateRetryTask's ON CONFLICT DO NOTHING is what
// makes losing that race harmless. Skipping early just avoids pointless work and
// records why no retry was made.
//
// Chat / quick-create tasks carry no issue_id, so the index cannot apply to them
// and their retries can never collide.
func hasRunnableSuccessor(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (bool, error) {
	if !task.IssueID.Valid {
		return false, nil
	}
	return q.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: task.IssueID,
		AgentID: task.AgentID,
	})
}

// MaybeRetryFailedTask spawns a fresh queued attempt for a recently-failed
// task when the failure was infrastructure-shaped (daemon crash, runtime
// went offline, dispatch/run timeout) and the task hasn't exhausted its
// max_attempts budget. The child task inherits agent/runtime/issue/chat
// links and, for resume-safe failures, the parent's session_id/work_dir so
// the agent can resume the conversation when the backend supports it. Returns
// the new task, or nil when no retry was created.
//
// Autopilot tasks are NOT auto-retried here; the autopilot scheduler owns
// its own re-run cadence and we don't want to double-fire it.
func (s *TaskService) MaybeRetryFailedTask(ctx context.Context, parent db.AgentTaskQueue) (*db.AgentTaskQueue, error) {
	if parent.Status != "failed" {
		return nil, nil
	}
	reason := ""
	if parent.FailureReason.Valid {
		reason = parent.FailureReason.String
	}
	if !retryableReasons[reason] {
		return nil, nil
	}
	// Use the reason-aware ceiling, not the raw max_attempts column, so an
	// orphaned provider_network task recovered on its 2nd attempt is still
	// allowed its deferred 3rd attempt (retryAttemptCeiling raises the ceiling
	// to 3). Kept in sync with retryEligible below, which applies the same
	// ceiling to the primary FailTask path.
	if parent.Attempt >= retryAttemptCeiling(reason, parent.MaxAttempts) {
		slog.Info("task auto-retry skipped: budget exhausted",
			"task_id", util.UUIDToString(parent.ID),
			"attempt", parent.Attempt,
			"max_attempts", parent.MaxAttempts,
			"ceiling", retryAttemptCeiling(reason, parent.MaxAttempts),
		)
		return nil, nil
	}
	// Autopilot has its own retry semantics (don't double-trigger) and a task
	// with no issue/chat link has nowhere to report its retry — retryEligible
	// covers both, keeping this sweeper path in sync with FailTask's in-tx retry.
	if !retryEligible(reason, parent) {
		return nil, nil
	}

	var runtimeMCPOverlay runtimeMCPOverlayData
	agent, agentErr := s.Queries.GetAgent(ctx, parent.AgentID)
	if agentErr != nil {
		// Best-effort: failing to resolve the agent for the overlay is not
		// retry-fatal. Log and continue — the daemon will reject the claim
		// later if the agent is genuinely gone.
		slog.Warn("task auto-retry: load agent for overlay failed",
			"parent_task_id", util.UUIDToString(parent.ID),
			"agent_id", util.UUIDToString(parent.AgentID),
			"error", agentErr,
		)
	} else {
		runtimeMCPOverlay = s.buildRuntimeMCPOverlay(ctx, parent.OriginatorUserID, agent)
	}
	// Mirror FailTask's in-tx backoff + effective-budget persistence: defer the
	// final provider_network attempt ~5s via fire_at (zero delay leaves fire_at
	// NULL for an immediate child), and write the reason-aware ceiling into the
	// child's max_attempts so the retry chain stays self-consistent.
	var retryFireAt pgtype.Timestamptz
	if delay := retryDelayForAttempt(reason, parent.Attempt); delay > 0 {
		retryFireAt = pgtype.Timestamptz{Time: time.Now().Add(delay), Valid: true}
	}
	// Same advisory slot check as FailTask's path, for the same reason: skip the
	// work when a successor is already visible. Losing the race is handled by
	// CreateRetryTask yielding the slot, which this caller reads as "no retry".
	if successor, herr := hasRunnableSuccessor(ctx, s.Queries, parent); herr != nil {
		slog.Warn("task auto-retry: successor check failed; attempting retry anyway",
			"parent_task_id", util.UUIDToString(parent.ID), "error", herr)
	} else if successor {
		slog.Info("task auto-retry skipped: a successor is already pending",
			"parent_task_id", util.UUIDToString(parent.ID),
			"issue_id", util.UUIDToString(parent.IssueID),
			"agent_id", util.UUIDToString(parent.AgentID),
		)
		return nil, nil
	}
	if s.TxStarter == nil {
		return nil, errors.New("task auto-retry: transaction starter is required")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("task auto-retry: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	child, err := qtx.CreateRetryTask(ctx, db.CreateRetryTaskParams{
		NewTaskID:            dbid.NewV7(),
		ID:                   parent.ID,
		FireAt:               retryFireAt,
		MaxAttempts:          pgtype.Int4{Int32: retryAttemptCeiling(reason, parent.MaxAttempts), Valid: true},
		RuntimeMcpOverlay:    runtimeMCPOverlay.Overlay,
		RuntimeConnectedApps: runtimeMCPOverlay.ConnectedApps,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Workspace torn down, or the pending slot was taken between the check
		// above and this insert. Same contract as FailTask's path: no retry, no
		// error.
		slog.Info("task auto-retry not created: no row written",
			"parent_task_id", util.UUIDToString(parent.ID),
			"reason", reason,
		)
		return nil, nil
	}
	if err != nil {
		slog.Warn("task auto-retry failed",
			"parent_task_id", util.UUIDToString(parent.ID),
			"reason", reason,
			"error", err,
		)
		return nil, err
	}
	if err := transferPendingSourceContextToRetry(ctx, qtx, parent, child); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Info("task auto-retry skipped: source context already transferred or attached",
				"parent_task_id", util.UUIDToString(parent.ID),
				"source_context_retry_id", util.UUIDToString(child.ID),
			)
			return nil, nil
		}
		return nil, err
	}
	if err := qtx.CopyChannelTaskDelivery(ctx, db.CopyChannelTaskDeliveryParams{
		ChildTaskID: child.ID, ParentTaskID: parent.ID,
	}); err != nil {
		return nil, fmt.Errorf("copy auto-retry channel delivery: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("task auto-retry: commit: %w", err)
	}
	slog.Info("task auto-retry enqueued",
		"parent_task_id", util.UUIDToString(parent.ID),
		"child_task_id", util.UUIDToString(child.ID),
		"reason", reason,
		"attempt", child.Attempt,
		"max_attempts", child.MaxAttempts,
		"status", child.Status,
	)
	// A queued child transitions ∅ → queued (same as EnqueueTaskFor*): broadcast
	// queued first, then notify the daemon — see EnqueueTaskForIssue for ordering
	// rationale. A deferred child (backoff armed) stays inert until
	// PromoteDueDeferredTasksForRuntime fires its queued event + wakeup.
	if child.Status == "queued" {
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, child)
		s.NotifyTaskEnqueued(ctx, child)
	}
	return &child, nil
}

func transferPendingSourceContextToRetry(ctx context.Context, q *db.Queries, parent, child db.AgentTaskQueue) error {
	if len(parent.Context) == 0 {
		return nil
	}
	var quickCreate QuickCreateContext
	if json.Unmarshal(parent.Context, &quickCreate) != nil || quickCreate.Type != QuickCreateContextType || quickCreate.SourceContextID == "" {
		return nil
	}
	workspaceID, err := util.ParseUUID(quickCreate.WorkspaceID)
	if err != nil {
		return err
	}
	contextID, err := util.ParseUUID(quickCreate.SourceContextID)
	if err != nil {
		return err
	}
	_, err = q.TransferPendingIssueSourceContextTask(ctx, db.TransferPendingIssueSourceContextTaskParams{
		NewTaskID: child.ID, WorkspaceID: workspaceID, ID: contextID, OldTaskID: parent.ID,
	})
	return err
}

// RerunIssue creates a fresh queued task for an agent on the issue. Used by
// the manual rerun endpoint.
//
// Target agent resolution:
//   - sourceTaskID Valid: rerun the agent that ran that task (and reuse its
//     leader/worker role). This is what the execution log retry button uses
//     so a per-row retry survives a subsequent assignee change and correctly
//     re-fires the squad worker or mention agent whose row was clicked. The
//     source task's trigger_comment_id is also inherited (when the caller
//     didn't pass one) so a per-row rerun of a comment- or mention-triggered
//     task stays comment-triggered — the daemon's buildCommentPrompt path
//     keys on TriggerCommentID, and losing it would degrade the rerun into
//     a generic issue run that no longer carries the original comment.
//   - sourceTaskID empty: fall back to the issue's current assignee (agent
//     or squad leader). This preserves the CLI / API contract for callers
//     that have an issue ID but no specific task to target.
//
// A retry ALWAYS reuses the source task's workdir when it still exists on
// disk (MUL-4869): a transient failure — network, provider 5xx/rate-limit,
// runtime_offline, timeout, or an auth/quota/config error the user has since
// fixed — should not throw away the work already done. Only the agent SESSION
// is conditionally resumed, and that decision is made later by the daemon claim
// handler from the SOURCE task (via rerun_of_task_id), NOT baked into this row.
// enqueueRerunTask pins force_fresh_session=true so an old claim handler during
// a rolling deploy degrades to a clean start rather than resuming a different
// execution; the new claim handler ignores the flag for reruns and resumes the
// session only when the source failure did not poison the conversation (see
// service.ResumeUnsafeFailure) and the source ran on the same runtime. When the
// dir is objectively unreusable (GC'd, absent on the claiming runtime, or never
// recorded) the daemon falls back to a fresh workdir. Auto-retry of an orphaned
// mid-flight failure (HandleFailedTasks → MaybeRetryFailedTask →
// CreateRetryTask) takes its own path, so MUL-1128's mid-flight resume contract
// is preserved.
//
// ErrRerunInvokeNotAllowed signals that RerunIssue refused to rerun because the
// current operator may not invoke the resolved target agent. The handler maps it
// to a structured 403 (no task was cancelled or created).
var ErrRerunInvokeNotAllowed = errors.New("rerun: operator not allowed to invoke target agent")

// Only tasks belonging to the target agent on this issue are cancelled.
// Tasks owned by other agents on the same issue (e.g. a parallel
// @-mention agent) are left alone — rerun must not collateral-cancel
// them.
//
// canInvoke re-validates that the current operator may invoke the RESOLVED
// target agent, keyed on the historical agent for a task_id rerun and on the
// current assignee/leader otherwise (MUL-4525). It runs AFTER the target is
// resolved but BEFORE any prior task is cancelled or a new one is created, so a
// caller who can see the issue but cannot invoke its private agent cannot use
// rerun as a back door — and a blocked rerun mutates nothing. Pass nil only
// from trusted internal callers (tests, backfill) that have already gated.
func (s *TaskService) RerunIssue(ctx context.Context, issueID pgtype.UUID, sourceTaskID pgtype.UUID, triggerCommentID pgtype.UUID, actorUserID pgtype.UUID, canInvoke func(agent db.Agent) bool) (*db.AgentTaskQueue, error) {
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("load issue: %w", err)
	}

	// Determine the target agent for the rerun.
	var (
		agentID             pgtype.UUID
		isLeader            bool
		squadID             pgtype.UUID
		coalescedCommentIDs []pgtype.UUID
	)
	if sourceTaskID.Valid {
		sourceTask, err := s.Queries.GetAgentTask(ctx, sourceTaskID)
		if err != nil {
			return nil, fmt.Errorf("load source task: %w", err)
		}
		if !sourceTask.IssueID.Valid || util.UUIDToString(sourceTask.IssueID) != util.UUIDToString(issueID) {
			return nil, fmt.Errorf("source task does not belong to this issue")
		}
		agentID = sourceTask.AgentID
		isLeader = sourceTask.IsLeaderTask
		// Carry the source task's squad provenance so a rerun of a leader
		// task still injects the squad briefing at claim time (see migration
		// 127 / daemon claim handler).
		squadID = sourceTask.SquadID
		// Inherit trigger provenance so a per-row rerun of a comment- or
		// mention-triggered task stays a comment-triggered task. Without
		// this the daemon's buildCommentPrompt path is skipped (it keys on
		// TriggerCommentID) and the rerun degrades into a generic issue
		// run that has lost the original comment context. Only override
		// when the caller didn't pass one explicitly.
		if !triggerCommentID.Valid {
			coalescedCommentIDs = append([]pgtype.UUID{}, sourceTask.CoalescedCommentIds...)
			if sourceTask.TriggerCommentID.Valid {
				triggerCommentID = sourceTask.TriggerCommentID
			} else if len(coalescedCommentIDs) > 0 {
				triggerCommentID, coalescedCommentIDs, err = s.promoteNewestSurvivingComment(ctx, coalescedCommentIDs)
				if err != nil {
					return nil, fmt.Errorf("repair source comment plan: %w", err)
				}
			}
		}
	} else {
		switch {
		case issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid:
			agentID = issue.AssigneeID
		case issue.AssigneeType.String == "squad" && issue.AssigneeID.Valid:
			squad, err := s.Queries.GetSquad(ctx, issue.AssigneeID)
			if err != nil {
				return nil, fmt.Errorf("issue is assigned to a squad but squad not found")
			}
			agentID = squad.LeaderID
			isLeader = true
			squadID = issue.AssigneeID
		default:
			return nil, fmt.Errorf("issue is not assigned to an agent or squad")
		}
	}

	// Re-validate invoke permission on the RESOLVED target before mutating
	// anything (MUL-4525). For a task_id rerun this gates the historical agent,
	// so a since-reassigned issue can't be used to re-fire a private agent the
	// operator may only view. A block fails closed: no prior task is cancelled,
	// no new task is created.
	if canInvoke != nil {
		targetAgent, err := s.Queries.GetAgent(ctx, agentID)
		if err != nil {
			return nil, fmt.Errorf("load target agent: %w", err)
		}
		if !canInvoke(targetAgent) {
			return nil, ErrRerunInvokeNotAllowed
		}
	}

	// Clear only the tasks that have not begun executing. Those are the rows the
	// fresh enqueue would collide with under
	// idx_one_pending_task_per_issue_agent_v2, and replacing them costs no work
	// while keeping the new run attributed to the rerunning member (MUL-4302 §5)
	// rather than inheriting whoever created the pending row.
	//
	// A running / waiting_local_directory task is deliberately left alone: an
	// agent is executing in it. Neither status appears in that unique index, so
	// the enqueue below inserts a queued row BEHIND the active one, and
	// ClaimAgentTask's per-(issue, agent) serialization holds it there until the
	// active run reaches a terminal state. Rerun used to cancel these too, so
	// asking an agent for another pass silently killed the pass it was still
	// working on; interrupting an in-flight run is what CancelTask /
	// `multica issue cancel-task` is for.
	clearPendingSlot := func() int {
		cancelled, cerr := s.Queries.CancelPendingTasksByIssueAndAgent(ctx, db.CancelPendingTasksByIssueAndAgentParams{
			IssueID: issueID,
			AgentID: agentID,
		})
		if cerr != nil {
			slog.Warn("rerun: cancel pending tasks failed",
				"issue_id", util.UUIDToString(issueID),
				"agent_id", util.UUIDToString(agentID),
				"error", cerr,
			)
		}
		for _, t := range cancelled {
			s.captureTaskCancelled(ctx, t)
			s.ReconcileAgentStatus(ctx, t.AgentID)
			s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, t)
		}
		return len(cancelled)
	}
	cancelledCount := clearPendingSlot()

	// A manual rerun is a NEW direct_human trigger attributed to the rerunning
	// member, not the original run's human (MUL-4302 §5); actorUserID carries them.
	// sourceTaskID is the rerun lineage: it rides the CreateAgentTask insert
	// (rerun_of_task_id) so the queued event / daemon claim never sees a NULL
	// lineage, and it stays distinct from system-retry's retry_of_task_id (§5).
	task, err := s.enqueueRerunTask(ctx, issue, agentID, triggerCommentID, coalescedCommentIDs, isLeader, squadID, actorUserID, sourceTaskID)
	if pendingSlotTakenErr(err) {
		// The clear above and this enqueue are separate commits, so a system
		// retry created by a concurrent FailTask can take the pending slot in
		// between. CreateRetryTask yields the slot when it is already occupied,
		// but it cannot yield to a row that does not exist yet, so a retry
		// committing inside this window gets there first. Clear once more and
		// retry: the deliberate human action is the one that should hold the
		// slot. Bounded to a single extra attempt — a second collision would mean
		// something is enqueueing in a loop, which is worth surfacing rather than
		// spinning on.
		slog.Info("issue rerun: pending slot taken concurrently, reclaiming",
			"issue_id", util.UUIDToString(issueID),
			"agent_id", util.UUIDToString(agentID),
		)
		cancelledCount += clearPendingSlot()
		task, err = s.enqueueRerunTask(ctx, issue, agentID, triggerCommentID, coalescedCommentIDs, isLeader, squadID, actorUserID, sourceTaskID)
	}
	if err != nil {
		return nil, err
	}
	slog.Info("issue rerun enqueued",
		"task_id", util.UUIDToString(task.ID),
		"issue_id", util.UUIDToString(issueID),
		"agent_id", util.UUIDToString(agentID),
		"source_task_id", util.UUIDToString(sourceTaskID),
		"is_leader", isLeader,
		"cancelled_pending", cancelledCount,
	)
	return &task, nil
}

// promoteNewestSurvivingComment repairs a manual rerun whose original trigger
// was deleted (the FK clears trigger_comment_id while the UUID-array plan
// survives). Promoting before enqueue lets the normal enqueue path recompute
// originator and user-scoped connected-app capabilities from the real comment,
// rather than carrying the deleted trigger's stale security context.
func (s *TaskService) promoteNewestSurvivingComment(ctx context.Context, ids []pgtype.UUID) (pgtype.UUID, []pgtype.UUID, error) {
	type survivingComment struct {
		id        pgtype.UUID
		createdAt time.Time
	}
	survivors := make([]survivingComment, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		key := util.UUIDToString(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		comment, err := s.Queries.GetComment(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return pgtype.UUID{}, nil, err
		}
		survivors = append(survivors, survivingComment{id: comment.ID, createdAt: comment.CreatedAt.Time})
	}
	if len(survivors) == 0 {
		return pgtype.UUID{}, nil, nil
	}
	newest := 0
	for i := 1; i < len(survivors); i++ {
		if survivors[i].createdAt.After(survivors[newest].createdAt) ||
			(survivors[i].createdAt.Equal(survivors[newest].createdAt) &&
				util.UUIDToString(survivors[i].id) > util.UUIDToString(survivors[newest].id)) {
			newest = i
		}
	}
	remaining := make([]pgtype.UUID, 0, len(survivors)-1)
	for i, comment := range survivors {
		if i != newest {
			remaining = append(remaining, comment.id)
		}
	}
	return survivors[newest].id, remaining, nil
}

// enqueueRerunTask enqueues a fresh task for the given agent on the issue.
// When the target agent is the issue's single-agent assignee we use the
// assignee-driven path (enqueueIssueTask) so the issue-assignee bookkeeping
// stays in sync; otherwise (squad member, prior assignee that has since been
// reassigned, mention agent) we use the mention path.
//
// force_fresh_session is pinned to true on every rerun row on purpose. It is
// the rollback-safe legacy signal: an OLD claim handler (mid rolling deploy)
// gates the whole resume lookup on !force_fresh_session, so it starts clean
// instead of resuming via the (agent, issue) most-recent query — which could
// pick a different execution than the one the user clicked. The NEW claim
// handler ignores this flag for reruns and instead reads the exact source task
// (rerun_of_task_id) to reuse its workdir and, when the failure did not poison
// the conversation, resume its session (MUL-4869).
func (s *TaskService) enqueueRerunTask(ctx context.Context, issue db.Issue, agentID pgtype.UUID, triggerCommentID pgtype.UUID, coalescedCommentIDs []pgtype.UUID, isLeader bool, squadID pgtype.UUID, actorUserID pgtype.UUID, rerunOfTaskID pgtype.UUID) (db.AgentTaskQueue, error) {
	if issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid &&
		util.UUIDToString(issue.AssigneeID) == util.UUIDToString(agentID) {
		return s.enqueueIssueTaskWithCommentPlan(ctx, issue, triggerCommentID, coalescedCommentIDs, true, "", actorUserID, rerunOfTaskID, pgtype.Timestamptz{})
	}
	return s.enqueueMentionTaskWithCommentPlan(ctx, issue, agentID, triggerCommentID, coalescedCommentIDs, isLeader, squadID, true, "", actorUserID, rerunOfTaskID)
}

// HandleFailedTasks runs the post-failure side effects for a batch of
// freshly-failed tasks: optional auto-retry, task:failed event broadcast,
// agent status reconciliation, and (when an issue has no remaining active
// task and isn't being retried) resetting the issue back to todo so the
// daemon can pick it up again.
//
// All callers that surface a task as failed — sweepers, FailTask,
// recover-orphans — funnel through here so the same UI-consistency
// guarantees apply on every code path.
func (s *TaskService) HandleFailedTasks(ctx context.Context, tasks []db.AgentTaskQueue) int {
	if len(tasks) == 0 {
		return 0
	}

	affectedAgents := make(map[string]pgtype.UUID)
	processedIssues := make(map[string]bool)
	retriedIssues := make(map[string]bool)
	retried := 0

	for _, t := range tasks {
		// Auto-retry first so the issue stays in_progress rather than
		// flapping todo → in_progress within a tick.
		retryPending := false
		if child, _ := s.MaybeRetryFailedTask(ctx, t); child != nil {
			retryPending = true
			retried++
			if t.IssueID.Valid {
				retriedIssues[util.UUIDToString(t.IssueID)] = true
			}
		}
		if !retryPending {
			if _, err := s.recoverDelegatedTaskFailure(ctx, t); err != nil {
				slog.Warn("handle failed tasks: delegated failure recovery failed",
					"task_id", util.UUIDToString(t.ID),
					"delegated_from_task_id", util.UUIDToString(t.DelegatedFromTaskID),
					"error", err,
				)
			}
		}

		failureReason := "agent_error"
		if t.FailureReason.Valid && t.FailureReason.String != "" {
			failureReason = t.FailureReason.String
		}
		s.captureTaskFailed(ctx, t)

		workspaceID := ""
		if t.IssueID.Valid {
			if issue, err := s.Queries.GetIssue(ctx, t.IssueID); err == nil {
				workspaceID = util.UUIDToString(issue.WorkspaceID)
				// Reset stuck in_progress issues only when no other active
				// task exists for the issue and no retry was just enqueued.
				issueKey := util.UUIDToString(t.IssueID)
				// Only "an agent is actively working" resets. in_review and
				// blocked are excluded — a human or an external dependency owns
				// the issue then — and a custom status resolves to the canonical
				// status it inherits, so a custom review gate is excluded for
				// the same reason In Review is. (MUL-6243)
				effectiveStatus := issuestatus.Effective(ctx, s.Queries, issue.WorkspaceID, issue.Status)
				if effectiveStatus == "in_progress" && !processedIssues[issueKey] && !retriedIssues[issueKey] {
					processedIssues[issueKey] = true
					hasActive, checkErr := s.Queries.HasActiveTaskForIssue(ctx, t.IssueID)
					if checkErr != nil {
						slog.Warn("handle failed tasks: active check failed",
							"issue_id", issueKey,
							"error", checkErr,
						)
					} else if !hasActive {
						updatedIssue, updateErr := s.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
							ID:          t.IssueID,
							Status:      "todo",
							WorkspaceID: issue.WorkspaceID,
						})
						if updateErr != nil {
							slog.Warn("handle failed tasks: reset stuck issue failed",
								"issue_id", issueKey,
								"error", updateErr,
							)
						} else {
							// This direct reset bypasses the HTTP UpdateIssue
							// handler that normally emits issue:updated, so emit
							// it here too. Without it the board / status-filter
							// caches keep showing the issue as in_progress until
							// the next write touches it (#4648 / MUL-3782).
							s.broadcastIssueUpdated(ctx, updatedIssue, issue.Status)
						}
					}
				}
			}
		}
		if workspaceID == "" {
			workspaceID = s.ResolveTaskWorkspaceID(ctx, t)
		}

		s.publishTaskFailedEvent(workspaceID, t, t.Error.String, failureReason, retryPending)

		affectedAgents[util.UUIDToString(t.AgentID)] = t.AgentID
	}

	for _, agentID := range affectedAgents {
		s.ReconcileAgentStatus(ctx, agentID)
	}
	s.notifyTasksFinished(tasks)
	return retried
}

const (
	delegatedFailureErrorSummaryRunes       = 800
	delegatedFailureRecoveryMaxTaskAttempts = 3
	delegatedFailureRecoveryCommentType     = "progress_update"
)

type delegatedFailureRecoveryDispatchOutcome uint8

const (
	delegatedFailureRecoveryCovered delegatedFailureRecoveryDispatchOutcome = iota
	delegatedFailureRecoveryReplayed
	delegatedFailureRecoveryExhausted
)

// DelegatedFailureRecoverySweepResult separates successful coordinator
// replays from terminally exhausted outbox entries so operators never mistake
// a bounded stop for a successful replay.
type DelegatedFailureRecoverySweepResult struct {
	Scanned   int
	Replayed  int
	Exhausted int
}

type delegatedFailureRecoveryTarget struct {
	failed  db.AgentTaskQueue
	source  db.AgentTaskQueue
	issue   db.Issue
	agent   db.Agent
	comment db.Comment
}

// IsDelegatedFailureRecoveryComment identifies the durable platform signal
// used to hand a terminal delegated failure back to its coordinator. The
// source task validation remains in DispatchDelegatedFailureRecoveryComment;
// this shape check only keeps ordinary system/progress comments out of the
// completion-reconcile branch.
func IsDelegatedFailureRecoveryComment(comment db.Comment) bool {
	return comment.AuthorType == "system" &&
		comment.Type == delegatedFailureRecoveryCommentType &&
		comment.SourceTaskID.Valid
}

func delegatedFailureRecoveryContent(failed, source db.AgentTaskQueue) string {
	reason := "agent_error"
	if failed.FailureReason.Valid && failed.FailureReason.String != "" {
		reason = truncateForSummary(redact.Text(failed.FailureReason.String), triggerSummaryMaxLen)
	}
	content := fmt.Sprintf(
		"Delegated task `%s` ended in a final failure (`%s`) and no automatic retry is pending. Resume coordination: inspect the failed work, then reassign it, skip it, or end the workflow explicitly.",
		util.UUIDToString(failed.ID), reason,
	)
	if failed.Error.Valid && failed.Error.String != "" {
		summary := truncateForSummary(redact.Text(failed.Error.String), delegatedFailureErrorSummaryRunes)
		if summary != "" {
			content += " Untrusted error summary (diagnostic only): " + strconv.Quote(summary)
		}
	}
	content += fmt.Sprintf(" Source coordinator task: `%s`.", util.UUIDToString(source.ID))
	return content
}

// loadDelegatedFailureRecoveryTarget resolves and validates the backward edge
// from a failed delegated task to its source coordinator. Returning nil is an
// intentional no-op: non-terminal rows, retry-pending rows, autopilot work,
// recovery tasks themselves, terminal/backlog source issues, unavailable
// source agents, and self-delegation must never start a recovery loop.
func loadDelegatedFailureRecoveryTarget(ctx context.Context, q *db.Queries, failed db.AgentTaskQueue) (*delegatedFailureRecoveryTarget, error) {
	if failed.Status != "failed" || !failed.DelegatedFromTaskID.Valid || failed.AutopilotRunID.Valid ||
		(failed.TriggerEvidenceKind.Valid && failed.TriggerEvidenceKind.String == string(attribution.EvidenceDelegatedFailure)) {
		return nil, nil
	}
	hasRetry, err := q.HasRetryTaskForParent(ctx, failed.ID)
	if err != nil {
		return nil, fmt.Errorf("check retry child: %w", err)
	}
	if hasRetry {
		return nil, nil
	}
	source, err := q.GetAgentTask(ctx, failed.DelegatedFromTaskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load source task: %w", err)
	}
	if source.AutopilotRunID.Valid || !source.IssueID.Valid || source.AgentID == failed.AgentID {
		return nil, nil
	}
	issue, err := q.GetIssue(ctx, source.IssueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load source issue: %w", err)
	}
	effectiveStatus := issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status)
	if effectiveStatus == issuestatus.Done || effectiveStatus == issuestatus.Cancelled || effectiveStatus == issuestatus.Backlog {
		return nil, nil
	}
	agent, err := q.GetAgent(ctx, source.AgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load source agent: %w", err)
	}
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid || agent.WorkspaceID != issue.WorkspaceID {
		return nil, nil
	}
	return &delegatedFailureRecoveryTarget{failed: failed, source: source, issue: issue, agent: agent}, nil
}

// ensureDelegatedFailureRecoveryComment creates one durable recovery signal
// per failed task. The failed row lock serializes FailTask and sweeper callers;
// the comment itself is platform-authored so it does not create subscriber or
// mention-notification side effects.
func (s *TaskService) ensureDelegatedFailureRecoveryComment(ctx context.Context, failedID pgtype.UUID) (*delegatedFailureRecoveryTarget, bool, error) {
	var target *delegatedFailureRecoveryTarget
	created := false
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		failed, err := qtx.GetAgentTaskForDelegatedFailureUpdate(ctx, failedID)
		if err != nil {
			return fmt.Errorf("lock failed task: %w", err)
		}
		target, err = loadDelegatedFailureRecoveryTarget(ctx, qtx, failed)
		if err != nil || target == nil {
			return err
		}
		comment, err := qtx.GetDelegatedFailureRecoveryComment(ctx, db.GetDelegatedFailureRecoveryCommentParams{
			IssueID:      target.issue.ID,
			WorkspaceID:  target.issue.WorkspaceID,
			SourceTaskID: failed.ID,
		})
		if err == nil {
			target.comment = comment
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("find recovery comment: %w", err)
		}
		createdComment, err := qtx.CreateComment(ctx, db.CreateCommentParams{
			ID:           dbid.NewV7(),
			IssueID:      target.issue.ID,
			WorkspaceID:  target.issue.WorkspaceID,
			AuthorType:   "system",
			AuthorID:     pgtype.UUID{Valid: true},
			Content:      delegatedFailureRecoveryContent(target.failed, target.source),
			Type:         delegatedFailureRecoveryCommentType,
			ParentID:     target.source.TriggerCommentID,
			SourceTaskID: failed.ID,
		})
		if err != nil {
			return fmt.Errorf("create recovery comment: %w", err)
		}
		target.comment = createdComment.Comment()
		created = true
		return nil
	}); err != nil {
		return nil, false, err
	}
	if target == nil {
		return nil, false, nil
	}
	if created && s.Bus != nil {
		s.Bus.Publish(events.Event{
			Type:        protocol.EventCommentCreated,
			WorkspaceID: util.UUIDToString(target.issue.WorkspaceID),
			ActorType:   "system",
			ActorID:     "",
			Payload: map[string]any{
				"comment":      commentEventFields(target.comment),
				"issue_title":  target.issue.Title,
				"issue_status": target.issue.Status,
			},
		})
	}
	return target, created, nil
}

func delegatedFailureRecoveryExhaustionContent(target *delegatedFailureRecoveryTarget) string {
	return fmt.Sprintf(
		"Automatic recovery for delegated task `%s` stopped after %d coordinator tasks ended before receiving the recovery signal. No more recovery tasks will be created automatically; resume or dismiss the work manually. Source coordinator task: `%s`.",
		util.UUIDToString(target.failed.ID),
		delegatedFailureRecoveryMaxTaskAttempts,
		util.UUIDToString(target.source.ID),
	)
}

func delegatedFailureRecoveryAttribution(target *delegatedFailureRecoveryTarget) (pgtype.UUID, pgtype.UUID) {
	originator := target.failed.OriginatorUserID
	accountable := target.failed.AccountableUserID
	if originator.Valid {
		accountable = originator
	}
	if !originator.Valid && !accountable.Valid {
		originator = target.source.OriginatorUserID
		accountable = target.source.AccountableUserID
		if originator.Valid {
			accountable = originator
		}
	}
	return originator, accountable
}

// exhaustDelegatedFailureRecovery atomically settles the recovery outbox after
// its bounded automatic attempts, creates one visible system explanation, and
// notifies the responsible human. The bool reports whether this caller created
// that terminal outcome. Updating the newest attempt first serializes
// concurrent sweepers; the second caller then observes the explanation written
// by the first and does not report another exhaustion.
func (s *TaskService) exhaustDelegatedFailureRecovery(ctx context.Context, target *delegatedFailureRecoveryTarget) (bool, error) {
	var exhaustedComment db.Comment
	var exhaustedInbox db.InboxItem
	created := false
	inboxCreated := false
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if _, err := qtx.AcknowledgeExhaustedDelegatedFailureRecovery(ctx, db.AcknowledgeExhaustedDelegatedFailureRecoveryParams{
			CommentID:    target.comment.ID,
			FailedTaskID: target.failed.ID,
			MaxAttempts:  delegatedFailureRecoveryMaxTaskAttempts,
		}); err != nil {
			return fmt.Errorf("acknowledge exhausted delegated failure recovery: %w", err)
		}

		comment, err := qtx.GetDelegatedFailureRecoveryExhaustionComment(ctx, db.GetDelegatedFailureRecoveryExhaustionCommentParams{
			IssueID:      target.issue.ID,
			WorkspaceID:  target.issue.WorkspaceID,
			SourceTaskID: target.failed.ID,
		})
		if err == nil {
			exhaustedComment = comment
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("find delegated failure exhaustion comment: %w", err)
		}

		createdComment, err := qtx.CreateComment(ctx, db.CreateCommentParams{
			ID:           dbid.NewV7(),
			IssueID:      target.issue.ID,
			WorkspaceID:  target.issue.WorkspaceID,
			AuthorType:   "system",
			AuthorID:     pgtype.UUID{Valid: true},
			Content:      delegatedFailureRecoveryExhaustionContent(target),
			Type:         "system",
			ParentID:     target.source.TriggerCommentID,
			SourceTaskID: target.failed.ID,
		})
		if err != nil {
			return fmt.Errorf("create delegated failure exhaustion comment: %w", err)
		}
		exhaustedComment = createdComment.Comment()
		created = true

		// Exhaustion deliberately does not @mention the coordinator agent: doing
		// so would enqueue a fourth recovery run and defeat the attempt bound.
		// Instead, create one durable action-required inbox item for the human
		// who originated (or is accountable for) the delegated work.
		_, recipient := delegatedFailureRecoveryAttribution(target)
		if recipient.Valid {
			_, err = qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID:      recipient,
				WorkspaceID: target.issue.WorkspaceID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("validate delegated failure exhaustion recipient: %w", err)
			}

			details, err := json.Marshal(map[string]any{
				"failed_task_id":       util.UUIDToString(target.failed.ID),
				"source_task_id":       util.UUIDToString(target.source.ID),
				"coordinator_agent_id": util.UUIDToString(target.agent.ID),
				"max_attempts":         delegatedFailureRecoveryMaxTaskAttempts,
			})
			if err != nil {
				return fmt.Errorf("encode delegated failure exhaustion details: %w", err)
			}
			exhaustedInbox, err = qtx.CreateInboxItem(ctx, db.CreateInboxItemParams{
				ID:            dbid.NewV7(),
				WorkspaceID:   target.issue.WorkspaceID,
				RecipientType: "member",
				RecipientID:   recipient,
				Type:          "task_failed",
				Severity:      "action_required",
				IssueID:       target.issue.ID,
				Title:         target.issue.Title,
				Body:          pgtype.Text{String: exhaustedComment.Content, Valid: true},
				ActorType:     pgtype.Text{String: "system", Valid: true},
				ActorID:       pgtype.UUID{},
				Details:       details,
			})
			if err != nil {
				return fmt.Errorf("create delegated failure exhaustion inbox item: %w", err)
			}
			inboxCreated = true
		}
		return nil
	}); err != nil {
		return false, err
	}

	if created && s.Bus != nil {
		s.Bus.Publish(events.Event{
			Type:        protocol.EventCommentCreated,
			WorkspaceID: util.UUIDToString(target.issue.WorkspaceID),
			ActorType:   "system",
			ActorID:     "",
			Payload: map[string]any{
				"comment":      commentEventFields(exhaustedComment),
				"issue_title":  target.issue.Title,
				"issue_status": target.issue.Status,
			},
		})
	}
	if inboxCreated && s.Bus != nil {
		s.Bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: util.UUIDToString(target.issue.WorkspaceID),
			ActorType:   "system",
			ActorID:     "",
			Payload: map[string]any{"item": map[string]any{
				"id":             util.UUIDToString(exhaustedInbox.ID),
				"workspace_id":   util.UUIDToString(exhaustedInbox.WorkspaceID),
				"recipient_type": exhaustedInbox.RecipientType,
				"recipient_id":   util.UUIDToString(exhaustedInbox.RecipientID),
				"type":           exhaustedInbox.Type,
				"severity":       exhaustedInbox.Severity,
				"issue_id":       util.UUIDToPtr(exhaustedInbox.IssueID),
				"title":          exhaustedInbox.Title,
				"body":           util.TextToPtr(exhaustedInbox.Body),
				"read":           exhaustedInbox.Read,
				"archived":       exhaustedInbox.Archived,
				"created_at":     util.TimestampToString(exhaustedInbox.CreatedAt),
				"actor_type":     util.TextToPtr(exhaustedInbox.ActorType),
				"actor_id":       util.UUIDToPtr(exhaustedInbox.ActorID),
				"details":        json.RawMessage(exhaustedInbox.Details),
				"issue_status":   target.issue.Status,
			}},
		})
	}
	return created, nil
}

// dispatchDelegatedFailureRecovery routes a recovery comment to the source
// coordinator without relying on generic mention parsing. A pre-claim task
// absorbs it; otherwise a dedicated queued successor is created. The only
// state that blocks both writes is a dispatched task (its claim is already
// built and the pending-task uniqueness slot is still held), so that narrow
// race records the comment as planned-but-undelivered and lets completion
// reconciliation schedule the follow-up. The three-pass loop closes state
// changes around those writes.
func (s *TaskService) dispatchDelegatedFailureRecovery(ctx context.Context, target *delegatedFailureRecoveryTarget, excludeTaskID pgtype.UUID) (delegatedFailureRecoveryDispatchOutcome, error) {
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		covered, err := s.Queries.HasTaskCoveringDelegatedFailureComment(ctx, db.HasTaskCoveringDelegatedFailureCommentParams{
			IssueID:       target.issue.ID,
			AgentID:       target.agent.ID,
			CommentID:     target.comment.ID,
			ExcludeTaskID: excludeTaskID,
		})
		if err != nil {
			return delegatedFailureRecoveryCovered, fmt.Errorf("check recovery coverage: %w", err)
		}
		if covered {
			return delegatedFailureRecoveryCovered, nil
		}

		recoveryTasks, err := s.Queries.CountDelegatedFailureRecoveryTasks(ctx, target.failed.ID)
		if err != nil {
			return delegatedFailureRecoveryCovered, fmt.Errorf("count delegated failure recovery tasks: %w", err)
		}
		if recoveryTasks >= delegatedFailureRecoveryMaxTaskAttempts {
			exhausted, err := s.exhaustDelegatedFailureRecovery(ctx, target)
			if err != nil {
				return delegatedFailureRecoveryCovered, err
			}
			if exhausted {
				return delegatedFailureRecoveryExhausted, nil
			}
			return delegatedFailureRecoveryCovered, nil
		}

		if merged, err := s.Queries.MergeDelegatedFailureCommentIntoPendingTask(ctx, db.MergeDelegatedFailureCommentIntoPendingTaskParams{
			CommentID:      target.comment.ID,
			TriggerSummary: s.buildCommentTriggerSummary(ctx, target.issue.WorkspaceID, target.comment.ID),
			IssueID:        target.issue.ID,
			AgentID:        target.agent.ID,
		}); err == nil {
			slog.Info("delegated failure recovery merged into pending coordinator task",
				"failed_task_id", util.UUIDToString(target.failed.ID),
				"coordinator_task_id", util.UUIDToString(merged.ID),
			)
			return delegatedFailureRecoveryReplayed, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return delegatedFailureRecoveryCovered, fmt.Errorf("merge recovery into pending task: %w", err)
		}

		originator, accountable := delegatedFailureRecoveryAttribution(target)
		source := attribution.SourceDelegation
		if !originator.Valid && !accountable.Valid {
			source = attribution.SourceUnattributed
		}
		ruleVersionID := target.failed.RuleVersionID
		if !ruleVersionID.Valid {
			ruleVersionID = target.source.RuleVersionID
		}
		overlay := s.buildRuntimeMCPOverlay(ctx, originator, target.agent)
		task, err := s.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
			ID:                   dbid.NewV7(),
			AgentID:              target.agent.ID,
			RuntimeID:            target.agent.RuntimeID,
			IssueID:              target.issue.ID,
			Priority:             priorityToInt(target.issue.Priority),
			TriggerCommentID:     target.comment.ID,
			TriggerSummary:       s.buildCommentTriggerSummary(ctx, target.issue.WorkspaceID, target.comment.ID),
			IsLeaderTask:         pgtype.Bool{Bool: target.source.IsLeaderTask, Valid: target.source.IsLeaderTask},
			SquadID:              target.source.SquadID,
			OriginatorUserID:     originator,
			AccountableUserID:    accountable,
			RuntimeMcpOverlay:    overlay.Overlay,
			RuntimeConnectedApps: overlay.ConnectedApps,
			OriginatorSource:     pgtype.Text{String: string(source), Valid: true},
			DelegatedFromTaskID:  target.failed.ID,
			RuleVersionID:        ruleVersionID,
			TriggerEvidenceKind:  pgtype.Text{String: string(attribution.EvidenceDelegatedFailure), Valid: true},
			TriggerEvidenceRefID: target.failed.ID,
			HeadSha:              headShaText(s.ResolveIssueReviewSHA(ctx, target.issue.ID)),
		})
		if err == nil {
			slog.Info("delegated failure recovery task enqueued",
				"failed_task_id", util.UUIDToString(target.failed.ID),
				"source_task_id", util.UUIDToString(target.source.ID),
				"recovery_task_id", util.UUIDToString(task.ID),
				"coordinator_agent_id", util.UUIDToString(target.agent.ID),
			)
			s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
			s.NotifyTaskEnqueued(ctx, task)
			return delegatedFailureRecoveryReplayed, nil
		}
		if !isDuplicatePendingTaskErr(err) {
			return delegatedFailureRecoveryCovered, fmt.Errorf("create recovery task: %w", err)
		}

		// A dispatched task still owns the unique queued/dispatched slot, but
		// its claim payload is immutable. Register the comment as undelivered so
		// its completion reconciliation creates the successor. Running tasks do
		// not own that slot, so they took the durable queued-successor path above.
		if active, err := s.Queries.RegisterPlannedCommentForActiveTask(ctx, db.RegisterPlannedCommentForActiveTaskParams{
			CommentID: target.comment.ID,
			IssueID:   target.issue.ID,
			AgentID:   target.agent.ID,
		}); err == nil {
			slog.Info("delegated failure recovery registered behind dispatched coordinator task",
				"failed_task_id", util.UUIDToString(target.failed.ID),
				"coordinator_task_id", util.UUIDToString(active.ID),
			)
			return delegatedFailureRecoveryReplayed, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return delegatedFailureRecoveryCovered, fmt.Errorf("register recovery on dispatched task: %w", err)
		}
	}
	return delegatedFailureRecoveryCovered, fmt.Errorf("delegate failure recovery could not acquire coordinator task slot")
}

// recoverDelegatedTaskFailure is the shared post-terminal hook for FailTask and
// HandleFailedTasks. handled reports whether the failure was an eligible
// delegated terminal; the production terminal paths deliberately retain their
// legacy raw-error notice alongside the richer coordinator recovery signal.
func (s *TaskService) recoverDelegatedTaskFailure(ctx context.Context, failed db.AgentTaskQueue) (handled bool, err error) {
	target, _, err := s.ensureDelegatedFailureRecoveryComment(ctx, failed.ID)
	if err != nil || target == nil {
		return false, err
	}
	_, err = s.dispatchDelegatedFailureRecovery(ctx, target, pgtype.UUID{})
	return true, err
}

// RecoverPendingDelegatedFailures replays the durable recovery outbox. The
// platform recovery comment is the obligation; it is complete only while a task
// that carries it can still execute, or after a task records it in
// delivered_comment_ids. This lets a later sweeper repair a process crash or
// transient database error between comment creation and coordinator dispatch
// without producing duplicate runnable tasks.
func (s *TaskService) RecoverPendingDelegatedFailures(ctx context.Context, maxPerTick int32) (DelegatedFailureRecoverySweepResult, error) {
	result := DelegatedFailureRecoverySweepResult{}
	if maxPerTick <= 0 {
		return result, nil
	}
	pending, err := s.Queries.ListPendingDelegatedFailureRecoveries(ctx, maxPerTick)
	if err != nil {
		return result, fmt.Errorf("list pending delegated failure recoveries: %w", err)
	}
	result.Scanned = len(pending)

	errs := make([]error, 0)
	for _, comment := range pending {
		outcome, recoveryErr := s.dispatchDelegatedFailureRecoveryComment(ctx, comment, pgtype.UUID{})
		if recoveryErr != nil {
			errs = append(errs, fmt.Errorf("dispatch recovery comment %s: %w", util.UUIDToString(comment.ID), recoveryErr))
			continue
		}
		switch outcome {
		case delegatedFailureRecoveryReplayed:
			result.Replayed++
		case delegatedFailureRecoveryExhausted:
			result.Exhausted++
		}
	}
	return result, errors.Join(errs...)
}

// DispatchDelegatedFailureRecoveryComment is used by completion reconciliation
// when a recovery signal arrived after a coordinator task was claimed. The
// completed task is excluded from the coverage check because the comment was
// planned but not delivered to it; routing then merges/enqueues exactly one
// follow-up.
func (s *TaskService) DispatchDelegatedFailureRecoveryComment(ctx context.Context, comment db.Comment, completedTaskID pgtype.UUID) error {
	_, err := s.dispatchDelegatedFailureRecoveryComment(ctx, comment, completedTaskID)
	return err
}

func (s *TaskService) dispatchDelegatedFailureRecoveryComment(ctx context.Context, comment db.Comment, completedTaskID pgtype.UUID) (delegatedFailureRecoveryDispatchOutcome, error) {
	if !IsDelegatedFailureRecoveryComment(comment) {
		return delegatedFailureRecoveryCovered, nil
	}
	failed, err := s.Queries.GetAgentTask(ctx, comment.SourceTaskID)
	if err != nil {
		return delegatedFailureRecoveryCovered, fmt.Errorf("load failed recovery source: %w", err)
	}
	target, err := loadDelegatedFailureRecoveryTarget(ctx, s.Queries, failed)
	if err != nil || target == nil {
		return delegatedFailureRecoveryCovered, err
	}
	if target.issue.ID != comment.IssueID || target.issue.WorkspaceID != comment.WorkspaceID {
		return delegatedFailureRecoveryCovered, fmt.Errorf("delegated failure recovery comment scope mismatch")
	}
	target.comment = comment
	return s.dispatchDelegatedFailureRecovery(ctx, target, completedTaskID)
}

// runInTx executes fn inside a single DB transaction. If TxStarter is nil
// (e.g. some tests construct TaskService directly), fn runs against the
// regular Queries handle without transactional guarantees.
func (s *TaskService) runInTx(ctx context.Context, fn func(*db.Queries) error) error {
	if s.TxStarter == nil {
		return fn(s.Queries)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReportProgress broadcasts a progress update via the event bus.
func (s *TaskService) ReportProgress(ctx context.Context, taskID string, workspaceID string, summary string, step, total int) {
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskProgress,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		TaskID:      taskID,
		Payload: protocol.TaskProgressPayload{
			TaskID:  taskID,
			Summary: summary,
			Step:    step,
			Total:   total,
		},
	})
}

// ReconcileAgentStatus refreshes agent status from the current working task
// set. The query returns no row when the status is already correct, which
// avoids rewriting updated_at and broadcasting a zero-information event.
func (s *TaskService) ReconcileAgentStatus(ctx context.Context, agentID pgtype.UUID) {
	agent, err := s.Queries.RefreshAgentStatusFromTasks(ctx, agentID)
	if err != nil {
		return
	}
	slog.Debug("agent status reconciled", "agent_id", util.UUIDToString(agentID), "status", agent.Status)
	s.publishAgentStatus(agent)
}

func (s *TaskService) updateAgentStatus(ctx context.Context, agentID pgtype.UUID, status string) {
	agent, err := s.Queries.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     agentID,
		Status: status,
	})
	if err != nil {
		return
	}
	s.publishAgentStatus(agent)
}

func (s *TaskService) publishAgentStatus(agent db.Agent) {
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAgentStatus,
		WorkspaceID: util.UUIDToString(agent.WorkspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload:     map[string]any{"agent": agentToMap(agent)},
	})
}

// LoadAgentSkills loads an agent's skills with their files for task execution.
func (s *TaskService) LoadAgentSkills(ctx context.Context, agentID pgtype.UUID) []AgentSkillData {
	skills, err := s.Queries.ListAgentSkills(ctx, agentID)
	if err != nil || len(skills) == 0 {
		return nil
	}

	result := make([]AgentSkillData, 0, len(skills))
	for _, sk := range skills {
		data := AgentSkillData{
			ID:          util.UUIDToString(sk.ID),
			Name:        sk.Name,
			Description: sk.Description,
			Content:     sk.Content,
		}
		files, _ := s.Queries.ListSkillFiles(ctx, sk.ID)
		for _, f := range files {
			data.Files = append(data.Files, AgentSkillFileData{Path: f.Path, Content: f.Content})
		}
		result = append(result, data)
	}
	return result
}

// LoadAgentSkillBundles returns every skill visible to an agent, including
// built-ins, with stable bundle hashes and lightweight refs for slim claims.
func (s *TaskService) LoadAgentSkillBundles(ctx context.Context, agentID pgtype.UUID) ([]AgentSkillData, []AgentSkillRefData) {
	skills := s.LoadAgentSkills(ctx, agentID)
	skills = append(skills, s.BuiltinSkills()...)
	return BuildAgentSkillBundles(skills)
}

func BuildAgentSkillBundles(skills []AgentSkillData) ([]AgentSkillData, []AgentSkillRefData) {
	bundles := make([]AgentSkillData, 0, len(skills))
	refs := make([]AgentSkillRefData, 0, len(skills))
	for _, skill := range skills {
		source := skill.Source
		id := skill.ID
		if source == "" {
			if id == "" {
				source = skillbundle.SourceBuiltin
			} else {
				source = skillbundle.SourceWorkspace
			}
		}
		if id == "" && source == skillbundle.SourceBuiltin {
			id = "builtin:" + skill.Name
		}
		skill.Source = source
		skill.ID = id

		files := make([]skillbundle.File, 0, len(skill.Files))
		for _, file := range skill.Files {
			files = append(files, skillbundle.File{Path: file.Path, Content: file.Content})
		}
		manifest := skillbundle.BuildManifest(skillbundle.Skill{
			ID:          skill.ID,
			Source:      skill.Source,
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
			Files:       files,
		})
		skill.Hash = manifest.Hash
		skill.SizeBytes = manifest.SizeBytes
		fileRefsByPath := make(map[string]skillbundle.FileRef, len(manifest.Files))
		for _, file := range manifest.Files {
			fileRefsByPath[file.Path] = file
		}
		for i := range skill.Files {
			if ref, ok := fileRefsByPath[skill.Files[i].Path]; ok {
				skill.Files[i].SHA256 = ref.SHA256
				skill.Files[i].SizeBytes = ref.SizeBytes
			}
		}
		bundles = append(bundles, skill)

		refFiles := make([]AgentSkillFileRefData, 0, len(manifest.Files))
		for _, file := range manifest.Files {
			refFiles = append(refFiles, AgentSkillFileRefData{
				Path:      file.Path,
				SHA256:    file.SHA256,
				SizeBytes: file.SizeBytes,
			})
		}
		refs = append(refs, AgentSkillRefData{
			ID:          skill.ID,
			Source:      skill.Source,
			Name:        skill.Name,
			Description: skill.Description,
			Hash:        manifest.Hash,
			SizeBytes:   manifest.SizeBytes,
			FileCount:   manifest.FileCount,
			Files:       refFiles,
		})
	}
	return bundles, refs
}

// AgentSkillData represents a skill for task execution responses.
type AgentSkillData struct {
	ID          string               `json:"id"`
	Source      string               `json:"source,omitempty"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Hash        string               `json:"hash,omitempty"`
	SizeBytes   int64                `json:"size_bytes,omitempty"`
	Content     string               `json:"content"`
	Files       []AgentSkillFileData `json:"files,omitempty"`
}

// AgentSkillFileData represents a supporting file within a skill.
type AgentSkillFileData struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	SHA256    string `json:"sha256,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

type AgentSkillRefData struct {
	ID          string                  `json:"id"`
	Source      string                  `json:"source"`
	Name        string                  `json:"name"`
	Description string                  `json:"description,omitempty"`
	Hash        string                  `json:"hash"`
	SizeBytes   int64                   `json:"size_bytes"`
	FileCount   int                     `json:"file_count"`
	Files       []AgentSkillFileRefData `json:"files,omitempty"`
}

type AgentSkillFileRefData struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// computeChatElapsedMs returns the wall-clock duration from task creation
// (user hit send) to terminal state (completed/failed). Stored on the
// assistant chat_message so the UI can render "Replied in 38s" /
// "Failed after 12s". Uses created_at — not started_at — because users
// experience total wait time, including queue + dispatch, not just the
// daemon's actual run time.
func computeChatElapsedMs(task db.AgentTaskQueue) pgtype.Int8 {
	if !task.CompletedAt.Valid || !task.CreatedAt.Valid {
		return pgtype.Int8{}
	}
	ms := task.CompletedAt.Time.Sub(task.CreatedAt.Time).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	return pgtype.Int8{Int64: ms, Valid: true}
}

func priorityToInt(p string) int32 {
	switch p {
	case "urgent":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// NotifyTaskEnqueued is the cross-package shim for callers outside
// TaskService (e.g. AutopilotService.dispatchRunOnly) that insert a
// row into agent_task_queue directly. Invalidates the empty-claim
// cache and kicks the daemon WS so the new task is claimed without
// waiting for the next poll.
func (s *TaskService) NotifyTaskEnqueued(ctx context.Context, task db.AgentTaskQueue) {
	s.captureTaskQueued(ctx, task)
	s.notifyTaskAvailable(task)
}

// NotifyTaskFinished invalidates a runtime's empty-claim verdict and emits a
// best-effort daemon wakeup after a task reaches a terminal state. The task ID
// is deliberately omitted from the wakeup payload: the completed task itself
// is not available; the hint only means that a queued successor may have
// become claimable because an agent-capacity or serialization barrier cleared.
func (s *TaskService) NotifyTaskFinished(task db.AgentTaskQueue) {
	s.forgetTaskReclaim(task)
	s.notifyRuntimeMayHaveWork(task.RuntimeID, "")
}

// notifyTasksFinished is the batch form used by bulk terminal transitions.
// Coalesce by runtime so cancelling many tasks on one machine produces one
// cache bump and one websocket hint rather than a burst of identical work.
func (s *TaskService) notifyTasksFinished(tasks []db.AgentTaskQueue) {
	seen := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if !task.RuntimeID.Valid {
			continue
		}
		s.forgetTaskReclaim(task)
		runtimeKey := util.UUIDToString(task.RuntimeID)
		if _, ok := seen[runtimeKey]; ok {
			continue
		}
		seen[runtimeKey] = struct{}{}
		s.notifyRuntimeMayHaveWork(task.RuntimeID, "")
	}
}

// notifyTaskAvailable runs after a task has been inserted: bumps the
// runtime's invalidation version so any in-flight claim that is about
// to write an "empty" verdict will have it rejected on read, then
// kicks the daemon WS so the daemon claims without waiting for its
// next poll. Order matters — Bump must happen before the wakeup,
// otherwise the wakeup-driven claim could read the still-current
// empty verdict and return null.
func (s *TaskService) notifyTaskAvailable(task db.AgentTaskQueue) {
	s.notifyRuntimeMayHaveWork(task.RuntimeID, util.UUIDToString(task.ID))
}

// notifyRuntimeMayHaveWork is the shared bump-before-wakeup primitive for both
// fresh enqueues and terminal transitions that can unblock queued work.
func (s *TaskService) notifyRuntimeMayHaveWork(runtimeID pgtype.UUID, taskID string) {
	if !runtimeID.Valid {
		return
	}
	runtimeKey := util.UUIDToString(runtimeID)
	// Use a background context: the cache bump / wakeup must outlive
	// the request that created the task, otherwise an early client
	// disconnect could leave the empty verdict in place and stall the
	// just-queued task until the TTL expires. The cache itself bounds
	// every Redis call with a short timeout so a wedged Redis cannot
	// block enqueue.
	s.EmptyClaim.Bump(context.Background(), runtimeKey)
	if s.Wakeup == nil {
		return
	}
	s.Wakeup.NotifyTaskAvailable(runtimeKey, taskID)
}

func (s *TaskService) broadcastTaskDispatch(ctx context.Context, task db.AgentTaskQueue) {
	var payload map[string]any
	if task.Context != nil {
		json.Unmarshal(task.Context, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["task_id"] = util.UUIDToString(task.ID)
	payload["runtime_id"] = util.UUIDToString(task.RuntimeID)
	payload["issue_id"] = util.UUIDToString(task.IssueID)
	payload["agent_id"] = util.UUIDToString(task.AgentID)
	// chat_session_id is the routing key the chat window uses to writethrough
	// `chatKeys.pendingTask` to status="running" the moment the daemon claims
	// the task. Without it the pill stays stuck at "Queued" until completion.
	if task.ChatSessionID.Valid {
		payload["chat_session_id"] = util.UUIDToString(task.ChatSessionID)
	}

	workspaceID := s.ResolveTaskWorkspaceID(ctx, task)
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskDispatch,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload:     payload,
	})
}

// taskEvent builds the shared task-lifecycle event contract. Scope hints are
// duplicated on the envelope intentionally: current listeners remain
// compatible with the payload map, while the realtime layer can route without
// decoding it once per-resource fanout is enabled.
func taskEvent(eventType, workspaceID string, task db.AgentTaskQueue, extra ...map[string]any) events.Event {
	payload := map[string]any{
		"task_id":  util.UUIDToString(task.ID),
		"agent_id": util.UUIDToString(task.AgentID),
		"issue_id": util.UUIDToString(task.IssueID),
		"status":   task.Status,
	}
	e := events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		TaskID:      util.UUIDToString(task.ID),
		Payload:     payload,
	}
	if task.ChatSessionID.Valid {
		chatSessionID := util.UUIDToString(task.ChatSessionID)
		payload["chat_session_id"] = chatSessionID
		e.ChatSessionID = chatSessionID
	}
	for _, fields := range extra {
		for key, value := range fields {
			payload[key] = value
		}
	}
	return e
}

func (s *TaskService) publishTaskEvent(eventType, workspaceID string, task db.AgentTaskQueue, extra ...map[string]any) {
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(taskEvent(eventType, workspaceID, task, extra...))
}

func (s *TaskService) broadcastTaskEvent(ctx context.Context, eventType string, task db.AgentTaskQueue, extra ...map[string]any) {
	workspaceID := s.ResolveTaskWorkspaceID(ctx, task)
	s.publishTaskEvent(eventType, workspaceID, task, extra...)
}

// taskFailedFields adds the terminal failure context required by channel
// outbounds without changing the long-standing map payload used by existing
// task event consumers. Error text is redacted and omitted while an automatic
// retry is pending, so consumers can distinguish an intermediate failed
// attempt from a user-visible terminal failure.
func taskFailedFields(errMsg, failureReason string, retryPending bool) map[string]any {
	fields := map[string]any{
		"failure_reason": failureReason,
		"retry_pending":  retryPending,
	}
	if errMsg != "" && !retryPending {
		fields["error"] = redact.Text(errMsg)
	}
	return fields
}

func (s *TaskService) publishTaskFailedEvent(workspaceID string, task db.AgentTaskQueue, errMsg, failureReason string, retryPending bool) {
	s.publishTaskEvent(protocol.EventTaskFailed, workspaceID, task, taskFailedFields(errMsg, failureReason, retryPending))
}

func (s *TaskService) broadcastTaskFailedEvent(ctx context.Context, task db.AgentTaskQueue, errMsg, failureReason string, retryPending bool) {
	workspaceID := s.ResolveTaskWorkspaceID(ctx, task)
	s.publishTaskFailedEvent(workspaceID, task, errMsg, failureReason, retryPending)
}

// ResolveTaskWorkspaceID determines the workspace ID for a task.
// For issue tasks, it comes from the issue. For chat tasks, from the chat session.
// For autopilot tasks, from the autopilot via its run.
// Returns "" when none of the links resolve — callers treat that as "not found".
func (s *TaskService) ResolveTaskWorkspaceID(ctx context.Context, task db.AgentTaskQueue) string {
	if task.IssueID.Valid {
		if issue, err := s.Queries.GetIssue(ctx, task.IssueID); err == nil {
			return util.UUIDToString(issue.WorkspaceID)
		}
	}
	if task.ChatSessionID.Valid {
		if cs, err := s.Queries.GetChatSession(ctx, task.ChatSessionID); err == nil {
			return util.UUIDToString(cs.WorkspaceID)
		}
	}
	if task.AutopilotRunID.Valid {
		if run, err := s.Queries.GetAutopilotRun(ctx, task.AutopilotRunID); err == nil {
			if ap, err := s.Queries.GetAutopilot(ctx, run.AutopilotID); err == nil {
				return util.UUIDToString(ap.WorkspaceID)
			}
		}
	}
	// Quick-create tasks have no issue / chat / autopilot link — workspace
	// lives in the context JSONB. Returning "" here is what blocked
	// requireDaemonTaskAccess (404 on /start, /progress, /complete, /fail
	// for the daemon) and silently dropped task:dispatch / task:completed
	// broadcasts, which is why quick-create tasks appeared stuck queued.
	if qc, ok := s.parseQuickCreateContext(task); ok {
		return qc.WorkspaceID
	}
	return ""
}

func (s *TaskService) broadcastChatDone(ctx context.Context, task db.AgentTaskQueue, msg *db.ChatMessage, quickActionsPending bool) {
	workspaceID := s.ResolveTaskWorkspaceID(ctx, task)
	if workspaceID == "" {
		return
	}
	payload := protocol.ChatDonePayload{
		ChatSessionID:       util.UUIDToString(task.ChatSessionID),
		TaskID:              util.UUIDToString(task.ID),
		QuickActionsPending: quickActionsPending,
	}
	if msg != nil {
		payload.MessageID = util.UUIDToString(msg.ID)
		payload.Content = msg.Content
		payload.MessageKind = msg.MessageKind
		if len(msg.QuickActions) > 0 {
			_ = json.Unmarshal(msg.QuickActions, &payload.QuickActions)
		}
		if msg.CreatedAt.Valid {
			payload.CreatedAt = msg.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if msg.ElapsedMs.Valid {
			payload.ElapsedMs = msg.ElapsedMs.Int64
		}
	}
	s.Bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		WorkspaceID:   workspaceID,
		ActorType:     "system",
		ActorID:       "",
		ChatSessionID: util.UUIDToString(task.ChatSessionID),
		Payload:       payload,
	})
}

// broadcastIssueUpdated publishes the issue:updated event the frontend's
// realtime reconcile (onIssueUpdated) relies on to move an issue between status
// columns / status filters and reconcile their bucket counts. prevStatus is the
// issue's status before the write so the client can gate that reconcile on
// status_changed.
//
// The `issue` payload is a map (IssueToMap), which the workspace WS fanout
// (listeners.go SubscribeAll) marshals and broadcasts as-is — that is what
// drives the UI reconcile. Note this does NOT cover the full HTTP UpdateIssue
// side effects: the activity-log and inbox listeners type-assert `issue` to a
// handler.IssueResponse and skip a map, so a background status reset does not
// emit status-change activity / notifications. That is intentional for the
// realtime-staleness fix (#4648 / MUL-3782); folding those side effects in
// would mean unifying the payload type and is left as a follow-up.
func (s *TaskService) broadcastIssueUpdated(ctx context.Context, issue db.Issue, prevStatus string) {
	prefix := s.getIssuePrefix(issue.WorkspaceID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"issue":          IssueToMapWithCategory(ctx, s.Queries, issue, prefix),
			"status_changed": prevStatus != issue.Status,
			"prev_status":    prevStatus,
		},
	})
}

func (s *TaskService) getIssuePrefix(workspaceID pgtype.UUID) string {
	ws, err := s.Queries.GetWorkspace(context.Background(), workspaceID)
	if err != nil {
		return ""
	}
	return ws.IssuePrefix
}

// commentEventFields renders the `comment` object carried by comment:created
// broadcasts published outside the HTTP handler (agent replies, delegated
// failure recovery, recovery exhaustion).
//
// created_at goes through util.TimestampToString — the SAME helper the REST
// CommentResponse uses — so the timestamp a client receives over WS is
// byte-identical to the one it gets when it refetches that comment. These
// payloads used to format the time with a literal "Z" suffix instead, which
// is not Go's Z07:00 offset token: pgx decodes timestamptz into the process
// location (pgtype/timestamptz.go, no ScanLocation configured), so on a
// deployment whose process TZ is not UTC the broadcast stamped local
// wall-clock digits with a UTC label. Clients then placed the entry a whole
// offset away from its real instant and moved it again once a refetch
// replaced the value — visible as timeline entries jumping position.
func commentEventFields(c db.Comment) map[string]any {
	return map[string]any{
		"id":             util.UUIDToString(c.ID),
		"issue_id":       util.UUIDToString(c.IssueID),
		"author_type":    c.AuthorType,
		"author_id":      util.UUIDToString(c.AuthorID),
		"content":        c.Content,
		"type":           c.Type,
		"parent_id":      util.UUIDToPtr(c.ParentID),
		"source_task_id": util.UUIDToPtr(c.SourceTaskID),
		"created_at":     util.TimestampToString(c.CreatedAt),
	}
}

func (s *TaskService) createAgentComment(ctx context.Context, issueID, agentID pgtype.UUID, content, commentType string, parentID, sourceTaskID pgtype.UUID) {
	if content == "" {
		return
	}
	// Look up issue to get workspace ID for mention expansion and broadcasting.
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	// Resolve the thread root for thread-level side effects without overwriting
	// parentID. The stored parent_id must remain the exact comment being replied
	// to; recursive thread reads recover the root when needed.
	var rootComment *db.Comment
	if parentID.Valid {
		if root, err := s.Queries.GetThreadRoot(ctx, db.GetThreadRootParams{
			CommentID:   parentID,
			WorkspaceID: issue.WorkspaceID,
		}); err == nil {
			rootComment = &root
		}
	}
	created, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:           dbid.NewV7(),
		IssueID:      issueID,
		WorkspaceID:  issue.WorkspaceID,
		AuthorType:   "agent",
		AuthorID:     agentID,
		Content:      content,
		Type:         commentType,
		ParentID:     parentID,
		SourceTaskID: sourceTaskID,
	})
	if err != nil {
		return
	}
	comment := created.Comment()
	s.CancelDeferredEscalationsForIssueAgent(ctx, issueID, agentID)
	commentFields := commentEventFields(comment)
	commentFields["revision"] = comment.Revision
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "agent",
		ActorID:     util.UUIDToString(agentID),
		Payload: map[string]any{
			"comment":        commentFields,
			"issue_title":    issue.Title,
			"issue_status":   issue.Status,
			"issue_revision": created.IssueRevision,
		},
	})
	s.AutoUnresolveThreadOnReply(ctx, rootComment, util.UUIDToString(issue.WorkspaceID), "agent", util.UUIDToString(agentID))
}

// AutoUnresolveThreadOnReply clears resolved_at on the thread root when a
// reply lands in a resolved thread, and broadcasts comment:unresolved. Shared
// between the user-facing Handler.CreateComment path and the agent-facing
// TaskService.createAgentComment path so the resolved-then-replied state can
// never desync (one of the bugs Emacs flagged on PR #2300). Errors are logged
// — the reply itself already committed, the desync is recoverable on next read.
func (s *TaskService) AutoUnresolveThreadOnReply(ctx context.Context, parent *db.Comment, workspaceID, actorType, actorID string) {
	if parent == nil || !parent.ResolvedAt.Valid {
		return
	}
	updated, err := s.Queries.UnresolveComment(ctx, parent.ID)
	if err != nil {
		slog.Warn("auto-unresolve on reply failed", "error", err, "comment_id", util.UUIDToString(parent.ID))
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentUnresolved,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload: map[string]any{
			"comment": map[string]any{
				"id":               util.UUIDToString(updated.ID),
				"issue_id":         util.UUIDToString(updated.IssueID),
				"author_type":      updated.AuthorType,
				"author_id":        util.UUIDToString(updated.AuthorID),
				"content":          updated.Content,
				"type":             updated.Type,
				"parent_id":        util.UUIDToPtr(updated.ParentID),
				"created_at":       util.TimestampToString(updated.CreatedAt),
				"updated_at":       util.TimestampToString(updated.UpdatedAt),
				"resolved_at":      util.TimestampToPtr(updated.ResolvedAt),
				"resolved_by_type": util.TextToPtr(updated.ResolvedByType),
				"resolved_by_id":   util.UUIDToPtr(updated.ResolvedByID),
				"revision":         updated.Revision,
			},
		},
	})
}

// IssueToMap renders an issue row as the map shape the issue:created /
// issue:updated broadcast payloads carry under their "issue" key. It is the
// single source of truth for that shape wherever the event is published from
// outside the HTTP handler — autopilot and the channel engine's /issue command
// on issue:created, the background stuck-issue status reset on issue:updated.
// The workspace WS fanout marshals it as-is for the UI, and cmd/server's
// extractIssueFields reads id / creator_id / workspace_id off it to decide who
// to auto-subscribe.
//
// The map must stay key-compatible with handler.IssueResponse, the other
// rendering of the same event. Clients type both as a complete Issue and
// insert it straight into the list cache without runtime validation, so a
// field missing here is a field that reads back undefined until the next
// refetch — see TestIssueToMap_KeysMatchIssueResponse, which fails if the two
// renderings drift apart.
// builtInStatusCategory returns a status's category when it can be known
// without a catalog read — i.e. for the 7 built-ins, where key == category.
func builtInStatusCategory(status string) string {
	if issuestatus.IsBuiltIn(status) {
		return status
	}
	return ""
}

// IssueToMapWithCategory is IssueToMap with an AUTHORITATIVE status_category,
// resolved through the catalog so a custom status is not emitted with a blank
// one. Background events go through here; clients treat this payload as a
// complete issue and bucket it by category. (MUL-6243)
func IssueToMapWithCategory(ctx context.Context, q issuestatus.Querier, issue db.Issue, issuePrefix string) map[string]any {
	m := IssueToMap(issue, issuePrefix)
	m["status_category"] = issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status)
	return m
}

func IssueToMap(issue db.Issue, issuePrefix string) map[string]any {
	return map[string]any{
		"id":           util.UUIDToString(issue.ID),
		"workspace_id": util.UUIDToString(issue.WorkspaceID),
		"number":       issue.Number,
		"identifier":   IssueIdentifier(issuePrefix, issue.Number),
		"title":        issue.Title,
		"description":  util.TextToPtr(issue.Description),
		"status":       issue.Status,
		// Mirrors handler.IssueResponse.StatusCategory: a built-in status IS
		// its own category, so this resolves with no catalog lookup. Empty for
		// a custom status, which consumers resolve via the catalog. (MUL-6243)
		"status_category":  builtInStatusCategory(issue.Status),
		"priority":         issue.Priority,
		"assignee_type":    util.TextToPtr(issue.AssigneeType),
		"assignee_id":      util.UUIDToPtr(issue.AssigneeID),
		"creator_type":     issue.CreatorType,
		"creator_id":       util.UUIDToString(issue.CreatorID),
		"parent_issue_id":  util.UUIDToPtr(issue.ParentIssueID),
		"project_id":       util.UUIDToPtr(issue.ProjectID),
		"position":         issue.Position,
		"stage":            util.Int4ToPtr(issue.Stage),
		"start_date":       util.DateToPtr(issue.StartDate),
		"due_date":         util.DateToPtr(issue.DueDate),
		"created_at":       util.TimestampToString(issue.CreatedAt),
		"updated_at":       util.TimestampToString(issue.UpdatedAt),
		"last_activity_at": util.TimestampToNanoPtr(issue.LastActivityAt),
		"revision":         issue.Revision,
		"metadata":         util.JSONObjectOrEmpty(issue.Metadata),
		"properties":       util.JSONObjectOrEmpty(issue.Properties),
	}
}

// IssueIdentifier renders the human-facing issue key ("MUL-42"). Callers that
// resolve the workspace prefix defensively may pass "": a failed workspace
// lookup should not surface as a stray "-42", so the number stands alone as
// "#42". The HTTP layer never passes "" — handler.getIssuePrefix derives a
// prefix from the workspace name before rendering anything.
func IssueIdentifier(issuePrefix string, number int32) string {
	if issuePrefix == "" {
		return "#" + strconv.Itoa(int(number))
	}
	return issuePrefix + "-" + strconv.Itoa(int(number))
}

// parseQuickCreateContext returns the quick-create payload if the task's
// context JSONB contains type == "quick_create"; otherwise the bool is
// false so callers can short-circuit. Tasks linked to an issue / chat /
// autopilot are never quick-create even if they happen to carry a
// context blob, so those are filtered up front.
func (s *TaskService) parseQuickCreateContext(task db.AgentTaskQueue) (QuickCreateContext, bool) {
	if task.IssueID.Valid || task.ChatSessionID.Valid || task.AutopilotRunID.Valid {
		return QuickCreateContext{}, false
	}
	if len(task.Context) == 0 {
		return QuickCreateContext{}, false
	}
	var qc QuickCreateContext
	if err := json.Unmarshal(task.Context, &qc); err != nil {
		return QuickCreateContext{}, false
	}
	if qc.Type != QuickCreateContextType {
		return QuickCreateContext{}, false
	}
	return qc, true
}

func (s *TaskService) sourceContextAttachedByTask(ctx context.Context, task db.AgentTaskQueue, qc QuickCreateContext) (bool, error) {
	if qc.SourceContextID == "" {
		return false, nil
	}
	workspaceID, workspaceErr := util.ParseUUID(qc.WorkspaceID)
	contextID, contextErr := util.ParseUUID(qc.SourceContextID)
	if workspaceErr != nil || contextErr != nil {
		return false, nil
	}
	row, err := s.Queries.GetIssueSourceContextByID(ctx, db.GetIssueSourceContextByIDParams{
		WorkspaceID: workspaceID,
		ID:          contextID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.State == "attached" && row.IssueID.Valid && row.OriginTaskID == task.ID, nil
}

// maxQuickCreateFailureDetailRunes bounds the failure reason lifted from a
// quick-create task's final output. A genuine CLI error (a duplicate message,
// a validation error) is short; an output far larger than this is a runaway
// raw-stream dump whose head is process narration rather than the real reason
// (same failure mode as GH #5455), so it is dropped to a generic notice rather
// than surfaced as the error.
const maxQuickCreateFailureDetailRunes = 2000

const quickCreateOversizedFailureDetail = "Quick create failed, but the agent's output was too large to show the reason safely. Check the task's execution log for details."

// quickCreateFailureDetail extracts a user-facing failure reason from a
// quick-create task's final output. The quick-create prompt instructs the agent
// to exit with the CLI error as its only output when `multica issue create`
// fails, so this normally carries the real reason (e.g. an active-duplicate
// message naming the existing issue). Returns "" when there is no usable output
// so the caller falls back to a generic message; redaction is applied by
// notifyQuickCreateFailed.
func quickCreateFailureDetail(result []byte) string {
	var payload protocol.TaskCompletedPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		return ""
	}
	// Same unescape as the comment-fallback path: literal `\n` sequences from
	// agent stdout become real newlines before the reason reaches the user.
	body := strings.TrimSpace(util.UnescapeBackslashEscapes(payload.Output))
	if body == "" {
		return ""
	}
	if utf8.RuneCountInString(body) > maxQuickCreateFailureDetailRunes {
		return quickCreateOversizedFailureDetail
	}
	return body
}

// notifyQuickCreateCompleted writes a success inbox notification to the
// requester pointing at the issue the agent just created. The issue is
// stamped with origin_type=quick_create + origin_id=<task_id> by the
// daemon-injected MULTICA_QUICK_CREATE_TASK_ID env var, so this lookup is
// deterministic — robust against the same agent creating other issues in
// parallel (e.g. assignment task running while max_concurrent_tasks > 1
// permits another quick-create alongside it).
func (s *TaskService) notifyQuickCreateCompleted(ctx context.Context, task db.AgentTaskQueue, qc QuickCreateContext, result []byte) {
	requesterID, err := util.ParseUUID(qc.RequesterID)
	if err != nil {
		slog.Warn("quick-create completion: invalid requester id", "task_id", util.UUIDToString(task.ID), "error", err)
		return
	}
	workspaceID, err := util.ParseUUID(qc.WorkspaceID)
	if err != nil {
		slog.Warn("quick-create completion: invalid workspace id", "task_id", util.UUIDToString(task.ID), "error", err)
		return
	}
	issue, err := s.Queries.GetIssueByOrigin(ctx, db.GetIssueByOriginParams{
		WorkspaceID: workspaceID,
		OriginType:  pgtype.Text{String: "quick_create", Valid: true},
		OriginID:    task.ID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			// The lookup itself failed (DB fault, timeout, …), not a confirmed
			// "no issue": the agent may well have created the issue, so a
			// failure inbox would misreport it. But the task is already
			// completed and nothing retries this reconciliation, so returning
			// silently would end the run with NO inbox result at all. Write a
			// neutral, terminal notification instead — the user always gets a
			// result, and it never asserts a failure we did not observe.
			slog.Error("quick-create completion: issue lookup failed, writing unconfirmed inbox",
				"task_id", util.UUIDToString(task.ID),
				"agent_id", util.UUIDToString(task.AgentID),
				"workspace_id", qc.WorkspaceID,
				"error", err,
			)
			s.notifyQuickCreateUnconfirmed(ctx, task, qc)
			return
		}
		// No issue created — the agent ran to completion but the CLI create
		// call must have failed (most often the active-duplicate guard). The
		// quick-create prompt tells the agent to exit with the CLI error as its
		// only output, so prefer that as the failure reason instead of a
		// generic string; fall back to notifyQuickCreateFailed's own default
		// when the output is empty. This is what turns the opaque "agent
		// finished without creating an issue" into the concrete reason (#5885).
		detail := quickCreateFailureDetail(result)
		slog.Warn("quick-create completion: no issue found, writing failure inbox",
			"task_id", util.UUIDToString(task.ID),
			"agent_id", util.UUIDToString(task.AgentID),
			"workspace_id", qc.WorkspaceID,
			"has_detail", detail != "",
		)
		s.notifyQuickCreateFailed(ctx, task, qc, detail)
		return
	}

	// Link the new issue back to this task so subsequent reads of the task
	// (Activity tab, Recent work, etc.) render it as a normal issue task
	// (kind = "direct") instead of staying on the "Creating issue" active-
	// wording label. Best-effort: a write failure here doesn't block the
	// inbox notification, which is the more important signal to the user.
	if err := s.Queries.LinkTaskToIssue(ctx, db.LinkTaskToIssueParams{
		ID:      task.ID,
		IssueID: issue.ID,
	}); err != nil {
		slog.Warn("quick-create completion: link task→issue failed",
			"task_id", util.UUIDToString(task.ID),
			"issue_id", util.UUIDToString(issue.ID),
			"error", err,
		)
	}

	// Subscribing the requester used to happen here, at completion. It now
	// happens at issue-creation time in the shared delegated-subscriber rule
	// (cmd/server/subscriber_listeners.go → subscribeDelegatedHuman), which
	// resolves the human from origin_type='quick_create' + the origin task's
	// originator_user_id — the same origin waterfall attribution uses.
	//
	// This was one of three separate hand-rolled fixes for "an agent created
	// this issue and no human is subscribed" (quick-create here, the autopilot
	// subscriber template, and — missing entirely until MUL-5483 — ordinary
	// agent-created sub-issues). Keeping a second write here would leave the
	// same decision encoded in two places that can drift.
	prefix := s.getIssuePrefix(workspaceID)
	identifier := fmt.Sprintf("%s-%d", prefix, issue.Number)
	details, _ := json.Marshal(map[string]any{
		"task_id":         util.UUIDToString(task.ID),
		"agent_id":        util.UUIDToString(task.AgentID),
		"issue_id":        util.UUIDToString(issue.ID),
		"identifier":      identifier,
		"original_prompt": qc.Prompt,
	})
	item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   workspaceID,
		RecipientType: "member",
		RecipientID:   requesterID,
		Type:          "quick_create_done",
		Severity:      "info",
		IssueID:       issue.ID,
		Title:         issue.Title,
		Body:          pgtype.Text{},
		ActorType:     pgtype.Text{String: "agent", Valid: true},
		ActorID:       task.AgentID,
		Details:       details,
	})
	if err != nil {
		slog.Error("quick-create completion: inbox write failed", "task_id", util.UUIDToString(task.ID), "error", err)
		return
	}
	s.publishQuickCreateInbox(item, qc.WorkspaceID, util.UUIDToString(task.AgentID), issue.Status)
}

// Inbox types for the two non-success quick-create outcomes. They are distinct
// because clients render them differently: the failed type carries an explicit
// "Failed:" framing, which must not be applied to an outcome we could not
// verify. Older clients that predate the unconfirmed type fall through their
// existing default branch and render the row's title/body unchanged, which is
// already the neutral wording.
const (
	inboxTypeQuickCreateFailed      = "quick_create_failed"
	inboxTypeQuickCreateUnconfirmed = "quick_create_unconfirmed"
)

// notifyQuickCreateFailed writes a failure inbox notification carrying the
// original prompt + agent ID so the frontend can render an "Edit as
// advanced form" entry that pre-fills the legacy create-issue modal
// without asking the user to retype. Use this only when the run is KNOWN not
// to have produced an issue; when that is merely unverified, use
// notifyQuickCreateUnconfirmed so the user is not told a definite failure.
func (s *TaskService) notifyQuickCreateFailed(ctx context.Context, task db.AgentTaskQueue, qc QuickCreateContext, errMsg string) {
	if errMsg == "" {
		errMsg = "Quick create did not finish successfully"
	}
	s.writeQuickCreateOutcomeInbox(ctx, task, qc, inboxTypeQuickCreateFailed, "Quick create failed", errMsg)
}

// quickCreateUnconfirmedDetail is the user-facing message for a quick-create
// run whose outcome could not be verified (the completion lookup itself
// failed). It must not claim failure: the agent may well have created the
// issue. It points at the one safe next step instead — check before retrying,
// so a retry cannot silently produce the duplicate the guard exists to prevent.
const quickCreateUnconfirmedDetail = "Couldn't confirm whether the issue was created. Check your recent issues before retrying — creating it again may produce a duplicate."

// notifyQuickCreateUnconfirmed writes a NEUTRAL terminal notification for a
// quick-create run whose outcome is unverified. The task is already completed
// and nothing re-runs this reconciliation, so returning without writing here
// would strand the requester with no inbox result at all — the failure mode
// this exists to prevent.
//
// It uses its own inbox type rather than reusing quick_create_failed: every
// client renders the failed type with a "Failed:" prefix, which would assert a
// failure we never observed no matter how neutral the title and body are.
// Severity stays action_required because the user does need to look.
func (s *TaskService) notifyQuickCreateUnconfirmed(ctx context.Context, task db.AgentTaskQueue, qc QuickCreateContext) {
	s.writeQuickCreateOutcomeInbox(ctx, task, qc, inboxTypeQuickCreateUnconfirmed, "Quick create needs a check", quickCreateUnconfirmedDetail)
}

// quickCreateNotifyTimeout bounds the detached terminal-notification write in
// writeQuickCreateOutcomeInbox. Long enough for a healthy DB round-trip, short
// enough that a wedged pool cannot pin the completion goroutine.
const quickCreateNotifyTimeout = 5 * time.Second

// writeQuickCreateOutcomeInbox writes the shared inbox row used by both
// non-success outcomes (known failure and unverified outcome). Callers own the
// user-facing wording and the row type; the row shape — original prompt, agent
// id, redacted message — is identical so the frontend's recovery affordance
// keeps working for both.
func (s *TaskService) writeQuickCreateOutcomeInbox(ctx context.Context, task db.AgentTaskQueue, qc QuickCreateContext, inboxType, title, errMsg string) {
	requesterID, err := util.ParseUUID(qc.RequesterID)
	if err != nil {
		return
	}
	workspaceID, err := util.ParseUUID(qc.WorkspaceID)
	if err != nil {
		return
	}
	// The task is already committed as completed and nothing retries this
	// notification, so it must not die with the caller's context. This matters
	// most on the unconfirmed path: the completion lookup may have failed
	// precisely BECAUSE ctx was cancelled or timed out, and reusing that ctx
	// would fail the write for the same reason — leaving the user with no
	// result at all, the exact silent drop this path exists to prevent. Detach
	// from cancellation, but keep a bound so a wedged DB cannot pin us.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), quickCreateNotifyTimeout)
	defer cancel()
	details, _ := json.Marshal(map[string]any{
		"task_id":           util.UUIDToString(task.ID),
		"agent_id":          util.UUIDToString(task.AgentID),
		"original_prompt":   qc.Prompt,
		"error":             redact.Text(errMsg),
		"source_context_id": qc.SourceContextID,
	})
	item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   workspaceID,
		RecipientType: "member",
		RecipientID:   requesterID,
		Type:          inboxType,
		Severity:      "action_required",
		IssueID:       pgtype.UUID{},
		Title:         title,
		Body:          pgtype.Text{String: redact.Text(errMsg), Valid: true},
		ActorType:     pgtype.Text{String: "agent", Valid: true},
		ActorID:       task.AgentID,
		Details:       details,
	})
	if err != nil {
		slog.Error("quick-create failure: inbox write failed", "task_id", util.UUIDToString(task.ID), "error", err)
		return
	}
	s.publishQuickCreateInbox(item, qc.WorkspaceID, util.UUIDToString(task.AgentID), "")
}

// publishQuickCreateInbox emits the WS event so the requester's inbox list
// updates immediately. Mirrors the payload shape used by the other inbox
// listeners (notification_listeners.go).
func (s *TaskService) publishQuickCreateInbox(item db.InboxItem, workspaceID, agentID, issueStatus string) {
	resp := map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
		"issue_status":   issueStatus,
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: workspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload:     map[string]any{"item": resp},
	})
}

// agentToMap builds a simple map for broadcasting agent status updates.
func agentToMap(a db.Agent) map[string]any {
	var rc any
	if a.RuntimeConfig != nil {
		json.Unmarshal(a.RuntimeConfig, &rc)
	}
	return map[string]any{
		"id":                   util.UUIDToString(a.ID),
		"workspace_id":         util.UUIDToString(a.WorkspaceID),
		"runtime_id":           util.UUIDToString(a.RuntimeID),
		"name":                 a.Name,
		"description":          a.Description,
		"avatar_url":           util.TextToPtr(a.AvatarUrl),
		"runtime_mode":         a.RuntimeMode,
		"runtime_config":       rc,
		"visibility":           a.Visibility,
		"status":               a.Status,
		"max_concurrent_tasks": a.MaxConcurrentTasks,
		"owner_id":             util.UUIDToPtr(a.OwnerID),
		"skills":               []any{},
		"created_at":           util.TimestampToString(a.CreatedAt),
		"updated_at":           util.TimestampToString(a.UpdatedAt),
		"archived_at":          util.TimestampToPtr(a.ArchivedAt),
		"archived_by":          util.UUIDToPtr(a.ArchivedBy),
	}
}
