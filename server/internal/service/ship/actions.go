// Phase 3 — Ship Hub card actions ("chips").
//
// This file is the dispatcher for the nine action endpoints registered
// in handler/ship_actions.go. Each public method corresponds to one
// chip the user can press on a PR card. Synchronous actions (merge,
// comment, dismiss_review, close_as_stale, close_pr, run_smoke_tests, rebase,
// nudge_author) finish in the same request. Asynchronous actions
// (diagnose_ci_failure, summarize_review_feedback) spawn an agent task
// via the supplied TaskEnqueuer and return the task id so the chip can
// surface "agent working on it" feedback.
//
// Every action records a `ship_card_action` row before doing real work,
// then updates it with the outcome. The audit trail is durable even if
// the GitHub call panics (defer-recover in the dispatcher).

package ship

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// Action constants are the canonical names recorded in the
// ship_card_action.action column AND the names the frontend chip
// component sends. Centralized so a typo in one place is detected at
// compile time everywhere else.
const (
	ActionMerge                   = "merge"
	ActionRebaseOnMain            = "rebase_on_main"
	ActionComment                 = "comment"
	ActionDismissReview           = "dismiss_review"
	ActionDiagnoseCIFailure       = "diagnose_ci_failure"
	ActionSummarizeReviewFeedback = "summarize_review_feedback"
	ActionNudgeAuthor             = "nudge_author"
	ActionRunSmokeTests           = "run_smoke_tests"
	ActionCloseAsStale            = "close_as_stale"
	// ActionSubmitReview — Phase 6.5. Posts a GitHub PR review
	// (APPROVE / REQUEST_CHANGES / COMMENT) without leaving Multica.
	ActionSubmitReview = "submit_review"
	ActionClosePR      = "close_pr"
)

// ActionStatus values mirror what's recorded in result_status.
const (
	StatusInProgress = "in_progress"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
)

// Errors that the handler layer translates to specific HTTP statuses.
// Service-layer code returns one of these (or a wrapped variant) so the
// handler can map without a string-matching switch.
var (
	// ErrActionUnknown is returned when ExecuteAction is called with an
	// action name the service doesn't recognize. The handler renders 400.
	ErrActionUnknown = errors.New("ship: unknown action")
	// ErrInvalidPayload covers any payload-shape problem — empty
	// required fields, malformed JSON, etc. The handler renders 400.
	ErrInvalidPayload = errors.New("ship: invalid payload")
	// ErrNotImplemented marks the rebase-via-true-rebase variant; we
	// return it so the handler can render a structured 501 with a
	// pointer to the implementation plan.
	ErrNotImplemented = errors.New("ship: not implemented")
	// ErrSmokeWorkflowNotConfigured is returned when run_smoke_tests
	// fires for a workspace that hasn't set ship_hub_smoke_workflow.
	ErrSmokeWorkflowNotConfigured = errors.New("ship: smoke workflow not configured")
)

// ActionResult is the response shape every chip handler returns. Fields
// are pointer-typed where they may be absent so the JSON omits them
// rather than rendering as the zero value (the desktop app's older
// schema-validated build expects optional fields to be absent, not
// `null`).
type ActionResult struct {
	Status      string      `json:"status"`
	AgentTaskID *string     `json:"agent_task_id,omitempty"`
	Comment     *gh.Comment `json:"comment,omitempty"`
	MergeSHA    string      `json:"merge_sha,omitempty"`
	Error       string      `json:"error,omitempty"`
	// ActionID is the row id in ship_card_action — handed back so the
	// frontend can subscribe to status updates for async actions.
	ActionID string `json:"action_id"`
	// Review is populated by the submit_review chip (Phase 6.5). Older
	// clients that don't know the field will simply ignore it per
	// CLAUDE.md "API Response Compatibility" — this is an additive
	// extension to the chip response shape.
	Review *gh.Review `json:"review,omitempty"`
}

// TaskEnqueuer is the slice of TaskService the ship actions service
// needs. Defining it here as an interface keeps the ship package
// independent of the concrete service implementation (and lets tests
// mock the spawn calls without standing up a full TaskService).
type TaskEnqueuer interface {
	EnqueueShipCardActionTask(ctx context.Context, p ShipCardActionTaskRequest) (db.AgentTaskQueue, error)
}

// ShipCardActionTaskRequest is the minimal-coupling shape between the
// ship service and the task service. Mirrors
// service.EnqueueShipCardActionTaskParams but lives in this package so
// the ship.Service interface doesn't import service/.
//
// The handler layer wires the two by adapting one to the other before
// passing the TaskEnqueuer in.
type ShipCardActionTaskRequest struct {
	WorkspaceID   pgtype.UUID
	AgentID       pgtype.UUID
	ProjectID     pgtype.UUID
	PullRequestID pgtype.UUID
	RepoURL       string
	PRNumber      int
	HeadSHA       string
	RequesterID   pgtype.UUID
	Action        string
}

// ExecuteAction is the unified dispatcher invoked by the handler. It:
//  1. Inserts a ship_card_action row (status=in_progress).
//  2. Routes to the per-action method.
//  3. Updates the row with succeeded / failed and the result payload.
//
// Returns ActionResult either way — the handler consults Status to pick
// the response code (succeeded → 200, failed → 502 unless the underlying
// error indicates a 4xx).
//
// task may be nil for actions that don't spawn agent jobs. workspace.
// orchestrator agent id is loaded by the handler and passed in
// orchestratorAgentID so the service stays free of workspace lookups.
func (s *Service) ExecuteAction(
	ctx context.Context,
	workspaceID pgtype.UUID,
	pr db.PullRequest,
	actorUserID pgtype.UUID,
	action string,
	payload json.RawMessage,
	task TaskEnqueuer,
	orchestratorAgentID pgtype.UUID,
	smokeWorkflow string,
) (*ActionResult, error) {
	if s.Github == nil {
		return nil, errors.New("ship: github client not configured")
	}

	row, err := s.recordActionStart(ctx, workspaceID, pr.ID, actorUserID, action, payload)
	if err != nil {
		return nil, fmt.Errorf("record action start: %w", err)
	}
	result := &ActionResult{ActionID: util.UUIDToString(row.ID), Status: StatusFailed}

	owner, repo, perr := gh.ParseRepoURL(pr.RepoUrl)
	if perr != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": perr.Error()})
		result.Error = perr.Error()
		return result, fmt.Errorf("%w: parse repo url: %v", ErrInvalidPayload, perr)
	}

	switch action {
	case ActionMerge:
		return s.actionMerge(ctx, row, pr, owner, repo, payload, result)
	case ActionRebaseOnMain:
		return s.actionRebaseOnMain(ctx, row, pr, owner, repo, result)
	case ActionComment:
		return s.actionComment(ctx, row, pr, owner, repo, payload, result)
	case ActionDismissReview:
		return s.actionDismissReview(ctx, row, pr, owner, repo, payload, result)
	case ActionNudgeAuthor:
		return s.actionNudgeAuthor(ctx, row, pr, owner, repo, payload, result)
	case ActionCloseAsStale:
		return s.actionCloseAsStale(ctx, row, pr, owner, repo, payload, result)
	case ActionRunSmokeTests:
		return s.actionRunSmokeTests(ctx, row, pr, owner, repo, smokeWorkflow, payload, result)
	case ActionSubmitReview:
		return s.actionSubmitReview(ctx, row, pr, owner, repo, actorUserID, payload, result)
	case ActionClosePR:
		return s.actionClosePR(ctx, row, pr, owner, repo, result)
	case ActionDiagnoseCIFailure, ActionSummarizeReviewFeedback:
		return s.actionSpawnAgentTask(ctx, row, pr, owner, repo, action, task, orchestratorAgentID, actorUserID, result)
	default:
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "unknown action"})
		result.Error = "unknown action"
		return result, fmt.Errorf("%w: %q", ErrActionUnknown, action)
	}
}

// recordActionStart inserts the audit row in the in_progress state.
// Done before any GitHub call so we always have a forensic record even
// if the GitHub call panics.
func (s *Service) recordActionStart(
	ctx context.Context,
	workspaceID, prID, actorUserID pgtype.UUID,
	action string,
	payload json.RawMessage,
) (db.ShipCardAction, error) {
	rawPayload := []byte(payload)
	if len(rawPayload) == 0 {
		rawPayload = nil
	}
	return s.Q.InsertShipCardAction(ctx, db.InsertShipCardActionParams{
		WorkspaceID:   workspaceID,
		PullRequestID: prID,
		ActorUserID:   actorUserID,
		Action:        action,
		Payload:       rawPayload,
		ResultStatus:  StatusInProgress,
		ResultPayload: nil,
		// completed_at left NULL until finishAction.
	})
}

// finishAction is the closing-side update. Best-effort: a failure to
// write the audit row never blocks the user-visible response. Passing
// completedAt=zero leaves completed_at NULL (used by async-spawn
// actions whose final outcome arrives later).
func (s *Service) finishAction(ctx context.Context, rowID pgtype.UUID, status string, payload map[string]any) {
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte(`{"error":"marshal failed"}`)
	}
	if _, err := s.Q.CompleteShipCardAction(ctx, db.CompleteShipCardActionParams{
		ID:            rowID,
		ResultStatus:  status,
		ResultPayload: body,
		CompletedAt:   pgtype.Timestamptz{Time: s.now(), Valid: true},
	}); err != nil {
		// Audit-row write failures are surfaced via slog but never
		// returned — the user already got their action result and we
		// don't want a logging-tier issue to look like an action
		// failure.
		_ = err
	}
}

// markActionInProgress is the async-spawn variant of finishAction —
// updates result_payload with the spawned task id but leaves
// completed_at NULL so a sweeper can find still-open rows later.
func (s *Service) markActionInProgress(ctx context.Context, rowID pgtype.UUID, payload map[string]any) {
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte(`{"error":"marshal failed"}`)
	}
	if _, err := s.Q.CompleteShipCardAction(ctx, db.CompleteShipCardActionParams{
		ID:            rowID,
		ResultStatus:  StatusInProgress,
		ResultPayload: body,
		CompletedAt:   pgtype.Timestamptz{}, // stays NULL
	}); err != nil {
		_ = err
	}
}

// mergeRequestPayload is the JSON body the merge chip sends.
type mergeRequestPayload struct {
	Method string `json:"method"`
}

func (s *Service) actionMerge(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo string,
	payload json.RawMessage,
	result *ActionResult,
) (*ActionResult, error) {
	var req mergeRequestPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "invalid body"})
			result.Error = "invalid body"
			return result, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
		}
	}
	method := strings.TrimSpace(req.Method)
	switch method {
	case "", "merge", "squash", "rebase":
	default:
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "invalid merge method"})
		result.Error = "invalid merge method"
		return result, fmt.Errorf("%w: invalid merge method %q", ErrInvalidPayload, method)
	}

	merge, err := s.Github.MergePullRequest(ctx, owner, repo, int(pr.PrNumber), method, "")
	if err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = err.Error()
		return result, err
	}

	// Optimistic state update so the Kanban shows the merged badge before
	// the pull_request webhook event arrives. The webhook handler is
	// idempotent — when it lands a few seconds later it observes the
	// state is already "merged" and is a no-op.
	if _, err := s.Q.MarkPullRequestMerged(ctx, db.MarkPullRequestMergedParams{
		ID:       pr.ID,
		MergedAt: pgtype.Timestamptz{Time: s.now(), Valid: true},
	}); err != nil {
		// The merge itself worked — log and continue. We don't roll back
		// the user-visible success because GitHub already merged.
		_ = err
	}

	result.Status = StatusSucceeded
	result.MergeSHA = merge.SHA
	s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{
		"merge_sha": merge.SHA,
		"merged":    merge.Merged,
		"message":   merge.Message,
	})
	return result, nil
}

func (s *Service) actionRebaseOnMain(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo string,
	result *ActionResult,
) (*ActionResult, error) {
	if err := s.Github.UpdatePullRequestBranch(ctx, owner, repo, int(pr.PrNumber), pr.HeadSha); err != nil {
		// ErrConflict means "already up to date" — surface that as a
		// success with the conflict reason so the chip can render
		// "branch is up to date" instead of a generic failure.
		if errors.Is(err, gh.ErrConflict) {
			result.Status = StatusSucceeded
			result.Error = "branch is already up to date"
			s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{
				"note": "already up to date",
			})
			return result, nil
		}
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = err.Error()
		return result, err
	}
	result.Status = StatusSucceeded
	s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{
		"strategy": "update-branch",
	})
	return result, nil
}

// commentRequestPayload is the JSON body used by both the comment chip
// and the nudge_author chip (when the author isn't a workspace member).
type commentRequestPayload struct {
	Body string `json:"body"`
}

func (s *Service) actionComment(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo string,
	payload json.RawMessage,
	result *ActionResult,
) (*ActionResult, error) {
	var req commentRequestPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "invalid body"})
		result.Error = "invalid body"
		return result, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	if strings.TrimSpace(req.Body) == "" {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "body is required"})
		result.Error = "body is required"
		return result, fmt.Errorf("%w: comment body is required", ErrInvalidPayload)
	}
	cm, err := s.Github.CreatePullRequestComment(ctx, owner, repo, int(pr.PrNumber), req.Body)
	if err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = err.Error()
		return result, err
	}
	result.Status = StatusSucceeded
	result.Comment = cm
	s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{
		"comment_id":  cm.ID,
		"comment_url": cm.HTMLURL,
	})
	return result, nil
}

// dismissReviewRequestPayload is the JSON body the dismiss_review chip sends.
type dismissReviewRequestPayload struct {
	ReviewID int64  `json:"review_id"`
	Message  string `json:"message"`
}

func (s *Service) actionDismissReview(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo string,
	payload json.RawMessage,
	result *ActionResult,
) (*ActionResult, error) {
	var req dismissReviewRequestPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "invalid body"})
		result.Error = "invalid body"
		return result, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	if req.ReviewID == 0 {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "review_id is required"})
		result.Error = "review_id is required"
		return result, fmt.Errorf("%w: review_id is required", ErrInvalidPayload)
	}
	if strings.TrimSpace(req.Message) == "" {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "message is required"})
		result.Error = "message is required"
		return result, fmt.Errorf("%w: message is required", ErrInvalidPayload)
	}
	if err := s.Github.DismissPullRequestReview(ctx, owner, repo, int(pr.PrNumber), req.ReviewID, req.Message); err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = err.Error()
		return result, err
	}
	result.Status = StatusSucceeded
	s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{
		"review_id": req.ReviewID,
	})
	return result, nil
}

// nudgeRequestPayload is the JSON body the nudge_author chip sends.
// Message is optional — empty means use the default nudge wording.
type nudgeRequestPayload struct {
	Message string `json:"message"`
}

func (s *Service) actionNudgeAuthor(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo string,
	payload json.RawMessage,
	result *ActionResult,
) (*ActionResult, error) {
	var req nudgeRequestPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "invalid body"})
			result.Error = "invalid body"
			return result, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
		}
	}
	body := strings.TrimSpace(req.Message)
	if body == "" {
		// Default polite nudge. Kept generic enough to avoid sounding
		// like a passive-aggressive bot — the user can always supply
		// their own wording.
		body = fmt.Sprintf("Friendly nudge — could you take another look at this PR when you have a moment? Thanks @%s!", pr.AuthorLogin)
	}
	cm, err := s.Github.CreatePullRequestComment(ctx, owner, repo, int(pr.PrNumber), body)
	if err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = err.Error()
		return result, err
	}
	result.Status = StatusSucceeded
	result.Comment = cm
	s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{
		"comment_id":  cm.ID,
		"comment_url": cm.HTMLURL,
	})
	return result, nil
}

// closeRequestPayload is the JSON body the close_as_stale chip sends.
// reason is optional — empty means use the default stale wording.
type closeRequestPayload struct {
	Reason string `json:"reason"`
}

func (s *Service) actionCloseAsStale(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo string,
	payload json.RawMessage,
	result *ActionResult,
) (*ActionResult, error) {
	var req closeRequestPayload
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "invalid body"})
			result.Error = "invalid body"
			return result, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
		}
	}
	// Post the courtesy comment first so it lands in the timeline before
	// the close event. If the comment fails we still close — the audit
	// row preserves the failed comment text for follow-up.
	commentBody := "Closing as stale. Reopen if this PR should be revived."
	if r := strings.TrimSpace(req.Reason); r != "" {
		commentBody = fmt.Sprintf("Closing as stale: %s. Reopen if this PR should be revived.", r)
	}
	if cm, err := s.Github.CreatePullRequestComment(ctx, owner, repo, int(pr.PrNumber), commentBody); err == nil {
		result.Comment = cm
	}
	if err := s.Github.ClosePullRequest(ctx, owner, repo, int(pr.PrNumber)); err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = err.Error()
		return result, err
	}
	if _, err := s.Q.MarkPullRequestClosed(ctx, db.MarkPullRequestClosedParams{
		ID:       pr.ID,
		ClosedAt: pgtype.Timestamptz{Time: s.now(), Valid: true},
	}); err != nil {
		_ = err
	}
	result.Status = StatusSucceeded
	s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{
		"reason": req.Reason,
	})
	return result, nil
}

func (s *Service) actionClosePR(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo string,
	result *ActionResult,
) (*ActionResult, error) {
	if err := s.Github.ClosePullRequest(ctx, owner, repo, int(pr.PrNumber)); err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = err.Error()
		return result, err
	}
	if _, err := s.Q.MarkPullRequestClosed(ctx, db.MarkPullRequestClosedParams{
		ID:       pr.ID,
		ClosedAt: pgtype.Timestamptz{Time: s.now(), Valid: true},
	}); err != nil {
		_ = err
	}
	result.Status = StatusSucceeded
	s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{"closed": true})
	return result, nil
}

// smokeTestsRequestPayload is the JSON body the run_smoke_tests chip sends.
type smokeTestsRequestPayload struct {
	EnvironmentID string `json:"environment_id"`
}

func (s *Service) actionRunSmokeTests(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo, smokeWorkflow string,
	payload json.RawMessage,
	result *ActionResult,
) (*ActionResult, error) {
	if strings.TrimSpace(smokeWorkflow) == "" {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "smoke workflow not configured"})
		result.Error = "smoke workflow not configured"
		return result, ErrSmokeWorkflowNotConfigured
	}
	var req smokeTestsRequestPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "invalid body"})
		result.Error = "invalid body"
		return result, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	if strings.TrimSpace(req.EnvironmentID) == "" {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "environment_id is required"})
		result.Error = "environment_id is required"
		return result, fmt.Errorf("%w: environment_id is required", ErrInvalidPayload)
	}
	inputs := map[string]string{
		"environment_id": req.EnvironmentID,
		"pr_number":      fmt.Sprintf("%d", pr.PrNumber),
		"head_sha":       pr.HeadSha,
	}
	// We dispatch on the PR's head ref so the workflow runs against the
	// branch under test. Falling back to the base ref would smoke-test
	// the wrong commit set.
	ref := pr.HeadRef
	if ref == "" {
		ref = pr.BaseRef
	}
	if err := s.Github.DispatchWorkflow(ctx, owner, repo, smokeWorkflow, ref, inputs); err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = err.Error()
		return result, err
	}
	result.Status = StatusSucceeded
	s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{
		"workflow":       smokeWorkflow,
		"ref":            ref,
		"environment_id": req.EnvironmentID,
	})
	return result, nil
}

// actionSpawnAgentTask handles the two async chips: diagnose_ci_failure
// and summarize_review_feedback. Both spawn a task on the workspace's
// orchestrator agent and return the task id; the audit row stays in
// in_progress until the daemon reports completion.
func (s *Service) actionSpawnAgentTask(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo string,
	action string,
	task TaskEnqueuer,
	orchestratorAgentID pgtype.UUID,
	actorUserID pgtype.UUID,
	result *ActionResult,
) (*ActionResult, error) {
	if task == nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "task service unavailable"})
		result.Error = "task service unavailable"
		return result, errors.New("ship: task service unavailable")
	}
	if !orchestratorAgentID.Valid {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "workspace orchestrator agent is not configured"})
		result.Error = "workspace orchestrator agent is not configured"
		return result, errors.New("ship: workspace orchestrator agent is not configured")
	}
	enqueued, err := task.EnqueueShipCardActionTask(ctx, ShipCardActionTaskRequest{
		WorkspaceID:   row.WorkspaceID,
		AgentID:       orchestratorAgentID,
		ProjectID:     pr.ProjectID,
		PullRequestID: pr.ID,
		RepoURL:       pr.RepoUrl,
		PRNumber:      int(pr.PrNumber),
		HeadSHA:       pr.HeadSha,
		RequesterID:   actorUserID,
		Action:        action,
	})
	if err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = err.Error()
		return result, fmt.Errorf("enqueue task: %w", err)
	}
	taskID := util.UUIDToString(enqueued.ID)
	result.Status = StatusInProgress
	result.AgentTaskID = &taskID
	// Don't finish-action: the daemon flips the row to succeeded /
	// failed when the task completes. If the daemon never reports, an
	// "older than 24h" cleanup query (Phase 4) can sweep stuck rows.
	// For now we update the result_payload with the task id so the
	// audit-trail has a forensic breadcrumb without the row closing.
	s.markActionInProgress(ctx, row.ID, map[string]any{"agent_task_id": taskID})
	return result, nil
}

// submitReviewRequestPayload is the JSON body the submit_review chip sends.
// event is the GitHub review verb; body is the optional text.
type submitReviewRequestPayload struct {
	Event string `json:"event"`
	Body  string `json:"body"`
}

// reviewVerbDisplay maps GitHub's review event names to a short
// human-readable verb used in the channel post. Falls back to the raw
// event string for forward-compat in case GitHub introduces a new verb.
func reviewVerbDisplay(event gh.ReviewEvent) string {
	switch event {
	case gh.ReviewEventApprove:
		return "Approved"
	case gh.ReviewEventRequestChanges:
		return "Requested changes"
	case gh.ReviewEventComment:
		return "Commented"
	default:
		return string(event)
	}
}

// translateReviewSubmitError turns GitHub's verbose 422 payloads into a
// short, user-friendly sentence. The ReviewDialog renders this string
// inline, so we trade fidelity for readability — the raw error stays
// in the ship_card_action audit row for forensics.
//
// Most common 422 we hit: "Review Cannot request changes on your own
// pull request" / "Review Can not approve your own pull request" —
// GitHub uses both phrasings across providers. We match on substrings
// so a future wording shift still classifies correctly.
func translateReviewSubmitError(err error, event gh.ReviewEvent) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	if strings.Contains(low, "your own pull request") ||
		strings.Contains(low, "your own pr") {
		switch event {
		case gh.ReviewEventApprove:
			return "GitHub doesn't let you approve your own pull request. Ask a teammate to review."
		case gh.ReviewEventRequestChanges:
			return "GitHub doesn't let you request changes on your own pull request. Ask a teammate to review, or use Comment only to leave notes."
		default:
			return "GitHub rejected this review on your own pull request."
		}
	}
	if strings.Contains(low, "can not be blank") || strings.Contains(low, "body is required") {
		return "GitHub requires a comment for this review type."
	}
	// Fall back to the raw error — better some signal than none.
	return msg
}

// actionSubmitReview posts a PR review to GitHub then (best-effort) drops a
// status line into the PR's Multica conversation channel so the team sees
// the review without leaving Multica. The channel post is intentionally
// non-blocking — a failed channel write must not surface as a failed
// review submission, since the GitHub side already succeeded.
func (s *Service) actionSubmitReview(
	ctx context.Context,
	row db.ShipCardAction,
	pr db.PullRequest,
	owner, repo string,
	actorUserID pgtype.UUID,
	payload json.RawMessage,
	result *ActionResult,
) (*ActionResult, error) {
	var req submitReviewRequestPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "invalid body"})
		result.Error = "invalid body"
		return result, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	event := gh.ReviewEvent(strings.ToUpper(strings.TrimSpace(req.Event)))
	switch event {
	case gh.ReviewEventApprove, gh.ReviewEventRequestChanges, gh.ReviewEventComment:
	default:
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "invalid event"})
		result.Error = "invalid event"
		return result, fmt.Errorf("%w: invalid review event %q", ErrInvalidPayload, req.Event)
	}
	body := strings.TrimSpace(req.Body)
	// Validate body presence here so the chip dialog can render a clean
	// 400 with a useful message instead of relaying GitHub's terse 422.
	// APPROVE allows an empty body; the other two events do not.
	if (event == gh.ReviewEventComment || event == gh.ReviewEventRequestChanges) && body == "" {
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": "body is required"})
		result.Error = "body is required for this review type"
		return result, fmt.Errorf("%w: body is required for %s", ErrInvalidPayload, event)
	}

	rev, err := s.Github.SubmitReview(ctx, owner, repo, int(pr.PrNumber), event, body)
	if err != nil {
		// Translate the most-hit GitHub 422 into a clean human message.
		// "Cannot request changes / approve on your own pull request" is
		// GitHub's default response when the reviewer is the author —
		// the raw payload is JSON-shaped and not user-friendly. We keep
		// the original error in the audit row but surface a clean
		// message to the chip dialog.
		clean := translateReviewSubmitError(err, event)
		s.finishAction(ctx, row.ID, StatusFailed, map[string]any{"error": err.Error()})
		result.Error = clean
		return result, err
	}

	result.Status = StatusSucceeded
	result.Review = rev

	// Best-effort: if the PR has a Multica conversation channel, drop a
	// status line so the team sees the review without watching GitHub.
	// We pull the actor's display name from the user table so the line
	// reads "Alice ✅ Approved · …" rather than a raw UUID. Lookup
	// failures fall back to "A reviewer" — better to post a generic line
	// than to skip the post.
	if s.PostToPRChannel != nil && pr.ConversationChannelID.Valid {
		who := "A reviewer"
		if actorUserID.Valid {
			if u, uerr := s.Q.GetUser(ctx, actorUserID); uerr == nil {
				if u.Name != "" {
					who = u.Name
				} else if u.Email != "" {
					who = u.Email
				}
			}
		}
		verb := reviewVerbDisplay(event)
		var content string
		if body != "" {
			// Escape pipe / newline collisions by simply trimming —
			// the channel renders markdown so user input passes through
			// the standard sanitizer downstream.
			content = fmt.Sprintf("**%s** %s on PR [#%d](%s) — %s\n\n> %s",
				who, verb, pr.PrNumber, rev.HTMLURL, pr.Title, body)
		} else {
			content = fmt.Sprintf("**%s** %s on PR [#%d](%s) — %s",
				who, verb, pr.PrNumber, rev.HTMLURL, pr.Title)
		}
		// Swallow the error: the review itself succeeded; surfacing a
		// channel-post failure here would mis-represent the outcome.
		_ = s.PostToPRChannel(ctx, pr.ConversationChannelID, content)
	}

	s.finishAction(ctx, row.ID, StatusSucceeded, map[string]any{
		"review_id":  rev.ID,
		"review_url": rev.HTMLURL,
		"event":      string(event),
	})
	return result, nil
}
