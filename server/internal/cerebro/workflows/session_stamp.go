package workflows

// FIR-2283 followup point (b): deterministic Plan/Build/Review session
// badge + rename on the run_skill / task-completion path.
//
// The problem: a workflow plan-phase run is dispatched as a quick_create task
// that stamps `loop_phase` on the task context (see actionRunSkill in
// actions.go and the loop revision dispatcher in loops/dispatch.go). The run's
// prompt ASKS the agent to call rename_session with name "Plan" and phase
// "plan", but that is advisory — when the agent forgets, the session never gets
// its phase badge or name, so the timeline shows a plain "Session N" and the UI
// can't tell which phase the run belonged to. E2E on staging confirmed 0
// sessions carried a phase on this path.
//
// The fix: on task completion, if the completed task carried `loop_phase`, look
// up the thread the run opened on the target issue and stamp its cerebro_session
// row with the phase + a display name — server-side, so the badge no longer
// depends on the agent remembering to call the tool. Best-effort throughout: a
// missing anchor or a write error is logged, never surfaced, and never blocks
// the task-completion transaction (which already committed before this runs).

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/sessionmode"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SessionPhaseStamper stamps a workflow phase badge + name onto the session a
// completed run_skill run opened. It talks to the shared pool directly (the
// comment lookup + cerebro_session upsert are both plain SQL); it holds no other
// state so it is safe to construct once and share.
type SessionPhaseStamper struct {
	pool *pgxpool.Pool
}

// NewSessionPhaseStamper builds a stamper over the shared pool. Wired onto
// TaskService.WorkflowSessionStamper in router.go; nil-safe at the call site so
// an unwired server just skips stamping.
func NewSessionPhaseStamper(pool *pgxpool.Pool) *SessionPhaseStamper {
	return &SessionPhaseStamper{pool: pool}
}

// taskPhaseContext is the narrow slice of a quick_create task's context this
// stamper reads. Every other quick_create field is ignored — only a task that
// carries loop_phase (a workflow phase dispatch) is a candidate for stamping.
type taskPhaseContext struct {
	LoopPhase             string `json:"loop_phase"`
	LoopSessionName       string `json:"loop_session_name"`
	WorkflowTargetIssueID string `json:"workflow_target_issue_id"`
}

// StampOnComplete is the seam TaskService.CompleteTask calls. It is a no-op for
// the overwhelming majority of tasks: only a quick_create dispatch that carried
// `loop_phase` (workflow plan/build/review phase) has anything to stamp.
func (s *SessionPhaseStamper) StampOnComplete(ctx context.Context, task db.AgentTaskQueue) {
	if s == nil || s.pool == nil || len(task.Context) == 0 {
		return
	}
	var tc taskPhaseContext
	if err := json.Unmarshal(task.Context, &tc); err != nil {
		return
	}
	phase := strings.TrimSpace(tc.LoopPhase)
	if phase == "" {
		return // not a phase dispatch — nothing to badge.
	}
	targetIssueID, err := util.ParseUUID(strings.TrimSpace(tc.WorkflowTargetIssueID))
	if err != nil {
		// A phase dispatch with no resolvable target issue can't be anchored to a
		// thread. Log once so a malformed dispatch is visible, then give up.
		slog.Warn("workflow session stamp: phase dispatch without a target issue",
			"task_id", uuidString(task.ID), "loop_phase", phase)
		return
	}
	if !task.AgentID.Valid {
		return
	}

	// Anchor preference: an issue-bound phase dispatch (the deterministic path)
	// sets TriggerCommentID to the kickoff comment it created — that IS the
	// session root, no guessing needed. Older detached dispatches fall back to
	// the first top-level comment the run's agent posted on the target issue
	// since the run started (thread = session, FIR-1874). CreatedAt covers a
	// null StartedAt (never-claimed tasks don't reach completion normally).
	rootCommentID := task.TriggerCommentID
	if !rootCommentID.Valid {
		since := task.StartedAt
		if !since.Valid {
			since = task.CreatedAt
		}
		err = s.pool.QueryRow(ctx, `
			SELECT id FROM comment
			WHERE issue_id = $1
			  AND author_type = 'agent'
			  AND author_id = $2
			  AND parent_id IS NULL
			  AND type = 'comment'
			  AND created_at >= $3
			ORDER BY created_at ASC, id ASC
			LIMIT 1`,
			targetIssueID, task.AgentID, since).Scan(&rootCommentID)
		if err != nil {
			if err == pgx.ErrNoRows {
				// The run posted no top-level comment (e.g. it only replied in an
				// existing thread, or produced nothing). Nothing to anchor a session
				// to — the agent's own rename_session remains the fallback.
				slog.Info("workflow session stamp: no thread root to badge",
					"issue_id", uuidString(targetIssueID), "loop_phase", phase)
				return
			}
			slog.Warn("workflow session stamp: thread-root lookup failed",
				"issue_id", uuidString(targetIssueID), "error", err)
			return
		}
	}

	badge, name := phaseBadgeAndName(phase, tc.LoopSessionName)
	if err := s.StampSession(ctx, targetIssueID, rootCommentID, badge, name); err != nil {
		slog.Warn("workflow session stamp: upsert failed",
			"issue_id", uuidString(targetIssueID),
			"root_comment_id", uuidString(rootCommentID), "error", err)
		return
	}
	slog.Info("workflow session stamped",
		"issue_id", uuidString(targetIssueID),
		"root_comment_id", uuidString(rootCommentID),
		"phase", badge, "name", name)
}

// StampSession upserts the session metadata row keyed on its thread root:
// badge `phase` + display `name`. ON CONFLICT the partial unique index
// (root_comment_id WHERE NOT NULL) turns a re-stamp into an update. The badge
// is always refreshed; the name only fills a blank one so a human- or
// agent-chosen name is never clobbered. Called at dispatch time by the
// workflow engine (deterministic badge the moment the phase starts) and at
// completion by StampOnComplete as the fallback. Satisfies loops.SessionStamper.
func (s *SessionPhaseStamper) StampSession(ctx context.Context, issueID, rootCommentID pgtype.UUID, phase, name string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	mode, ok := sessionmode.Normalize(phase)
	if !ok {
		mode = sessionmode.Build
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO cerebro_session (issue_id, root_comment_id, position, name, phase, mode)
		VALUES ($1, $2, 0, $3, $4, $5)
		ON CONFLICT (root_comment_id) WHERE root_comment_id IS NOT NULL
		DO UPDATE SET
			phase = EXCLUDED.phase,
			mode = EXCLUDED.mode,
			name = CASE WHEN cerebro_session.name = '' THEN EXCLUDED.name ELSE cerebro_session.name END,
			updated_at = now()`,
		issueID, rootCommentID, name, phase, mode)
	return err
}

// phaseBadgeAndName derives the stored badge and the display name from the
// dispatched phase label and an optional session name. The badge is the canonical
// lower-case phase word ("plan" / "build" / "review") so the UI matches
// rename_session's contract; a multi-word label like "Build 2" keeps only its
// leading word for the badge but supplies the full "Build 2" as the name. The
// explicit loop_session_name wins for the display name when present.
func phaseBadgeAndName(phase, sessionName string) (badge, name string) {
	lower := strings.ToLower(strings.TrimSpace(phase))
	badge = lower
	if fields := strings.Fields(lower); len(fields) > 0 {
		badge = fields[0]
	}
	name = strings.TrimSpace(sessionName)
	if name == "" {
		name = titleFirst(strings.TrimSpace(phase))
	}
	return badge, name
}

// titleFirst upper-cases the first rune of s and leaves the rest untouched
// ("plan" -> "Plan"). Empty stays empty.
func titleFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
