package github

// HTTP handler for POST /api/webhooks/github.
//
// Pipeline:
//   1. Read body, verify HMAC against GITHUB_APP_WEBHOOK_SECRET (constant-time).
//   2. Dedup on X-GitHub-Delivery via github_webhook_delivery table.
//   3. Decode the event by X-GitHub-Event header.
//   4. Look up workspace_repo_binding by repo full name.
//   5. Resolve issue:
//      - For pull_request events, extract identifier from branch/title/body
//        and load the issue, OR look up by stored pr_repo+pr_number.
//      - For review / review_thread events, look up by pr_repo+pr_number.
//   6. For review/thread events, fetch CR predicate from GitHub API.
//   7. Build state-machine Input, call Decide, apply the Decision in a TX:
//        - ActionLinkPR: SetIssuePR (+ optionally update status)
//        - ActionSetStatus: UpdateIssueStatus
//      Then write activity_log row.
//   8. Publish issue:updated + activity:created on the bus for WS broadcast.
//
// All non-2xx responses include a one-line reason in the body to help with
// GitHub's webhook delivery debugger UI. Successful no-ops return 200 with
// {"ok":true,"action":"noop"}.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	dbtx "github.com/multica-ai/multica/server/pkg/db"
	audit "github.com/multica-ai/multica/server/pkg/db/audit"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// WebhookHandler holds the dependencies needed to process GitHub webhook
// deliveries.
type WebhookHandler struct {
	Queries   *db.Queries
	TxStarter dbtx.TxStarter
	Bus       *events.Bus
	Auth      *AppAuth

	// Secret is the App-level webhook secret loaded from
	// GITHUB_APP_WEBHOOK_SECRET. Empty disables HMAC verification — only
	// safe in tests.
	Secret string

	// NewClient overrides client construction in tests.
	NewClient func(installationID int64) PRReviewClient
}

// NewWebhookHandlerFromEnv constructs the handler using GITHUB_APP_*
// environment variables.
func NewWebhookHandlerFromEnv(queries *db.Queries, txStarter dbtx.TxStarter, bus *events.Bus) (*WebhookHandler, error) {
	auth, err := NewAppAuthFromEnv()
	if err != nil {
		return nil, err
	}
	secret := os.Getenv("GITHUB_APP_WEBHOOK_SECRET")
	if secret == "" {
		return nil, errors.New("GITHUB_APP_WEBHOOK_SECRET must be set")
	}
	return &WebhookHandler{
		Queries:   queries,
		TxStarter: txStarter,
		Bus:       bus,
		Auth:      auth,
		Secret:    secret,
	}, nil
}

// ServeHTTP is the entrypoint registered on the router.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5*1024*1024)) // 5 MiB cap
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	if h.Secret != "" {
		if !verifySignature(r.Header.Get("X-Hub-Signature-256"), h.Secret, body) {
			writeErr(w, http.StatusUnauthorized, "invalid signature")
			return
		}
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	eventType := r.Header.Get("X-GitHub-Event")
	if deliveryID == "" || eventType == "" {
		writeErr(w, http.StatusBadRequest, "missing X-GitHub-Delivery or X-GitHub-Event header")
		return
	}

	// We only care about four event types. Everything else is a fast 200.
	// pull_request_review_comment fires for each inline comment CR (or any
	// reviewer) leaves on a specific file/line; we mirror those rows into
	// issue_review_thread so the dev agent can walk them.
	relevant := eventType == "pull_request" ||
		eventType == "pull_request_review" ||
		eventType == "pull_request_review_thread" ||
		eventType == "pull_request_review_comment" ||
		eventType == "check_run" ||
		eventType == "issue_comment"
	if !relevant {
		writeOK(w, "ignored", map[string]any{"event": eventType})
		return
	}

	// Best-effort dedup. If RecordWebhookDelivery returns no row, this is
	// a redelivery — return 200 so GitHub stops retrying. We tag the
	// repo from the payload below; for now use placeholder until we parse.
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		writeErr(w, http.StatusBadRequest, "decode payload: "+err.Error())
		return
	}
	repoFullName, err := extractRepo(payload)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	_, err = h.Queries.RecordWebhookDelivery(ctx, db.RecordWebhookDeliveryParams{
		DeliveryID: deliveryID,
		Repo:       repoFullName,
		Event:      eventType,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// ON CONFLICT DO NOTHING returns no rows on duplicate.
			writeOK(w, "duplicate", map[string]any{"delivery_id": deliveryID})
			return
		}
		slog.Error("webhook: record delivery failed", "delivery_id", deliveryID, "error", err)
		writeErr(w, http.StatusInternalServerError, "dedup write failed")
		return
	}

	binding, err := h.Queries.GetRepoBindingByRepo(ctx, repoFullName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeOK(w, "no_binding", map[string]any{"repo": repoFullName})
			return
		}
		writeErr(w, http.StatusInternalServerError, "lookup binding: "+err.Error())
		return
	}
	if !binding.Active {
		writeOK(w, "binding_inactive", map[string]any{"repo": repoFullName})
		return
	}

	resp, err := h.dispatch(ctx, eventType, payload, binding)
	if err != nil {
		slog.Error("webhook: dispatch failed", "delivery_id", deliveryID, "event", eventType, "error", err)
		writeErr(w, http.StatusInternalServerError, "dispatch: "+err.Error())
		return
	}
	writeOK(w, resp.label, resp.fields)
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

type dispatchResult struct {
	label  string
	fields map[string]any
}

func (h *WebhookHandler) dispatch(ctx context.Context, eventType string, payload map[string]json.RawMessage, binding db.WorkspaceRepoBinding) (*dispatchResult, error) {
	switch eventType {
	case "pull_request":
		return h.handlePR(ctx, payload, binding)
	case "pull_request_review":
		return h.handleReview(ctx, payload, binding)
	case "pull_request_review_thread":
		return h.handleReviewThread(ctx, payload, binding)
	case "pull_request_review_comment":
		return h.handleReviewComment(ctx, payload, binding)
	case "check_run":
		return h.handleCheckRun(ctx, payload, binding)
	case "issue_comment":
		return h.handleIssueComment(ctx, payload, binding)
	}
	return &dispatchResult{label: "ignored"}, nil
}

func (h *WebhookHandler) handlePR(ctx context.Context, payload map[string]json.RawMessage, binding db.WorkspaceRepoBinding) (*dispatchResult, error) {
	var p prPayload
	if err := decode(payload, "action", &p.Action); err != nil {
		return nil, err
	}
	if err := decode(payload, "pull_request", &p.PR); err != nil {
		return nil, err
	}
	// sender is best-effort: missing on some synthetic payloads.
	_ = decode(payload, "sender", &p.Sender)

	// Resolve issue: prefer existing PR linkage, else extract identifier
	// from headRef/title/body and look up.
	issue, found, err := h.resolveIssueForPR(ctx, binding, p.PR)
	if err != nil {
		return nil, err
	}
	if !found {
		return &dispatchResult{label: "issue_not_found", fields: map[string]any{
			"pr_repo": binding.RepoFullName, "pr_number": p.PR.Number,
		}}, nil
	}

	in := Input{
		Kind:               EventKindPR,
		IssueStatus:        issue.Status,
		PRAction:           PRAction(p.Action),
		Merged:             p.PR.Merged,
		SenderLogin:        p.Sender.Login,
		SecondsSinceOpened: secondsSincePROpened(p.PR.CreatedAt),
	}
	dec := Decide(in)
	if dec.Action == ActionNoop {
		return &dispatchResult{label: "noop", fields: map[string]any{"issue": issue.ID.String(), "current": issue.Status}}, nil
	}

	res, err := h.applyDecision(ctx, issue, dec, p.PR, binding, "pull_request")
	if err != nil || res == nil {
		return res, err
	}

	// If the merge transitioned coderabbit → staged (CR not installed on
	// the repo, race ahead of predicate, etc.), chain a follow-up staged →
	// done so the final card state matches reality. This mirrors the
	// user-expected flow: CR clean → staged → human merges → done.
	// Idempotent for normal merges (where the issue was already at staged).
	if dec.NewStatus == StatusStaged && dec.ActivityKind == "pr_merged_from_coderabbit" {
		refetched, ferr := h.Queries.GetIssue(ctx, issue.ID)
		if ferr == nil && refetched.Status == StatusStaged {
			followupIn := Input{
				Kind:        EventKindPR,
				IssueStatus: refetched.Status,
				PRAction:    PRAction(p.Action),
				Merged:      p.PR.Merged,
				SenderLogin: p.Sender.Login,
			}
			followupDec := Decide(followupIn)
			if followupDec.Action != ActionNoop && followupDec.NewStatus == StatusDone {
				_, _ = h.applyDecision(ctx, refetched, followupDec, p.PR, binding, "pull_request_chained")
			}
		}
	}
	return res, nil
}

func (h *WebhookHandler) handleReview(ctx context.Context, payload map[string]json.RawMessage, binding db.WorkspaceRepoBinding) (*dispatchResult, error) {
	var p reviewPayload
	if err := decode(payload, "action", &p.Action); err != nil {
		return nil, err
	}
	if err := decode(payload, "review", &p.Review); err != nil {
		return nil, err
	}
	if err := decode(payload, "pull_request", &p.PR); err != nil {
		return nil, err
	}
	issue, found, err := h.resolveIssueByPR(ctx, binding.RepoFullName, p.PR.Number)
	if err != nil {
		return nil, err
	}
	if !found {
		return &dispatchResult{label: "issue_not_found", fields: map[string]any{"pr_number": p.PR.Number}}, nil
	}

	if strings.EqualFold(p.Review.User.Login, binding.CrBotUsername) {
		h.recordCRSignal(ctx, issue, p.PR.HTMLURL, p.PR.Head.SHA, "review", p.Action, map[string]any{
			"review_id": p.Review.ID,
			"state":     p.Review.State,
		})
	}

	if p.Action != "submitted" {
		return &dispatchResult{label: "review_recorded", fields: map[string]any{"reason": "non-submitted review action"}}, nil
	}

	if issue.Status != StatusCoderabbit {
		slog.Info("webhook: cr loop v2 review event mirrored only outside coderabbit",
			"issue", uuidStr(issue.ID),
			"status", issue.Status,
			"review_id", p.Review.ID,
			"review_state", p.Review.State,
		)
		return &dispatchResult{label: "v2_silent_mirror_review", fields: map[string]any{
			"issue":        issue.ID.String(),
			"status":       issue.Status,
			"review_state": p.Review.State,
		}}, nil
	}

	// Bulk-mirror the review's inline findings (CR only) before predicate +
	// Decide. GitHub delivers `pull_request_review` ahead of the per-finding
	// `pull_request_review_comment` webhooks, so without this fetch the
	// LocalUnresolvedThreadCount branch in decideReview would race the count
	// to zero and noop. Pulling them via REST is the canonical source — the
	// per-comment webhooks become idempotent re-deliveries afterward.
	// Failure degrades to the per-comment-driven path in v1. Under v2 the
	// local mirror is load-bearing, so failures route fail-closed to blocked.
	if strings.EqualFold(p.Review.User.Login, binding.CrBotUsername) {
		if merr := h.bulkMirrorReviewComments(ctx, binding, issue, p.PR.Number, p.Review.ID); merr != nil {
			slog.Error("webhook: bulk-mirror failed under v2; failing closed",
				"issue", uuidStr(issue.ID),
				"review_id", p.Review.ID,
				"error", merr,
			)
			return h.failClosedToBlocked(ctx, issue, p.PR, binding, "cr_bulk_mirror_failed: "+merr.Error())
		}
	}

	noOpenChanges, noUnresolved, unresolvedCount, err := h.predicate(ctx, binding, p.PR.Number, issue.ID)
	if err != nil {
		return nil, err
	}

	in := Input{
		Kind:                       EventKindReview,
		IssueStatus:                issue.Status,
		ReviewState:                ReviewState(strings.ToLower(p.Review.State)),
		ReviewByCR:                 strings.EqualFold(p.Review.User.Login, binding.CrBotUsername),
		NoOpenCRChangesRequest:     noOpenChanges,
		NoUnresolvedCRThreads:      noUnresolved,
		LocalUnresolvedThreadCount: unresolvedCount,
	}
	dec := Decide(in)
	if dec.Action == ActionNoop {
		return &dispatchResult{label: "noop", fields: map[string]any{"issue": issue.ID.String(), "review_state": p.Review.State}}, nil
	}
	return h.applyDecision(ctx, issue, dec, p.PR, binding, "pull_request_review")
}

func (h *WebhookHandler) handleReviewThread(ctx context.Context, payload map[string]json.RawMessage, binding db.WorkspaceRepoBinding) (*dispatchResult, error) {
	var p reviewThreadPayload
	if err := decode(payload, "action", &p.Action); err != nil {
		return nil, err
	}
	if err := decode(payload, "pull_request", &p.PR); err != nil {
		return nil, err
	}
	// `thread` is best-effort — older payloads may omit it. We use it to mirror
	// resolved/unresolved state into issue_review_thread keyed on the node_id.
	_ = decode(payload, "thread", &p.Thread)

	issue, found, err := h.resolveIssueByPR(ctx, binding.RepoFullName, p.PR.Number)
	if err != nil {
		return nil, err
	}
	if !found {
		return &dispatchResult{label: "issue_not_found"}, nil
	}

	// Mirror resolved/unresolved state to our local issue_review_thread rows.
	// GitHub's pull_request_review_thread payload carries the GraphQL node_id
	// for the thread *and* the numeric ids of every comment in the thread, so
	// we have two ways to find our rows. We use the comment ids as the primary
	// key (they're the natural unique key in our table) and stamp node_id onto
	// the row at the same time so future deliveries can use either.
	switch p.Action {
	case "resolved", "unresolved":
		newState := p.Action // both "resolved" and "unresolved" are valid state values
		nodeIDArg := pgtype.Text{Valid: false}
		if p.Thread.NodeID != "" {
			nodeIDArg = pgtypeText(p.Thread.NodeID)
			// Best-effort update by node_id first, in case rows were created by
			// a previous resolved/unresolved delivery that stamped node_id.
			_, _ = h.Queries.SetReviewThreadStateByThreadNodeID(ctx, db.SetReviewThreadStateByThreadNodeIDParams{
				GhThreadNodeID: pgtypeText(p.Thread.NodeID),
				State:          newState,
				AgentID:        pgtype.UUID{Valid: false},
			})
		}
		for _, c := range p.Thread.Comments {
			if c.ID == 0 {
				continue
			}
			_, _ = h.Queries.SetReviewThreadStateByCommentID(ctx, db.SetReviewThreadStateByCommentIDParams{
				GhCommentID:    c.ID,
				State:          newState,
				GhThreadNodeID: nodeIDArg,
				AgentID:        pgtype.UUID{Valid: false},
			})
		}
	}

	if reviewThreadAuthoredByCR(ctx, h.Queries, p.Thread, binding.CrBotUsername) {
		h.recordCRSignal(ctx, issue, p.PR.HTMLURL, p.PR.Head.SHA, "thread", p.Action, map[string]any{
			"thread_node_id": p.Thread.NodeID,
			"comment_count":  len(p.Thread.Comments),
		})
	}

	return &dispatchResult{
		label:  "review_thread_recorded",
		fields: map[string]any{"issue": uuidStr(issue.ID)},
	}, nil
}

// handleReviewComment mirrors a single CR review comment (a per-line PR
// comment) into issue_review_thread. We only insert rows authored by the
// configured CR bot — human inline comments are tracked differently.
//
// Action `created` and `edited` upsert; `deleted` is currently best-effort
// ignored (we leave the row in place; the dev agent can resolve via thread
// resolution anyway).
func (h *WebhookHandler) handleReviewComment(ctx context.Context, payload map[string]json.RawMessage, binding db.WorkspaceRepoBinding) (*dispatchResult, error) {
	var p reviewCommentPayload
	if err := decode(payload, "action", &p.Action); err != nil {
		return nil, err
	}
	if err := decode(payload, "comment", &p.Comment); err != nil {
		return nil, err
	}
	if err := decode(payload, "pull_request", &p.PR); err != nil {
		return nil, err
	}

	// Only act on creation/edits. Deletion is rare from CR and we keep the
	// row so the audit trail is preserved.
	if p.Action != "created" && p.Action != "edited" {
		return &dispatchResult{label: "noop", fields: map[string]any{"reason": "non-created/edited comment"}}, nil
	}

	// Only mirror comments authored by the configured CR bot. Human PR
	// comments aren't part of the dev-agent fixing loop today.
	if !strings.EqualFold(p.Comment.User.Login, binding.CrBotUsername) {
		return &dispatchResult{label: "noop", fields: map[string]any{"reason": "non-CR author", "author": p.Comment.User.Login}}, nil
	}

	// Drop CR auto-ack replies ("Skipped: comment is from another GitHub
	// bot.") — these are CR's response to *our* fixer-reply posts and they
	// generate new review_thread rows that the resolver then tries to
	// process, creating an infinite bot-loop. The marker comment
	// `<!-- This is an auto-generated reply by CodeRabbit -->` is unique
	// to these auto-replies; legitimate review findings never carry it.
	if isCRAutoAckBody(p.Comment.Body) {
		return &dispatchResult{label: "noop", fields: map[string]any{"reason": "cr_auto_ack", "gh_comment_id": p.Comment.ID}}, nil
	}

	issue, found, err := h.resolveIssueByPR(ctx, binding.RepoFullName, p.PR.Number)
	if err != nil {
		return nil, err
	}
	if !found {
		return &dispatchResult{label: "issue_not_found", fields: map[string]any{"pr_number": p.PR.Number}}, nil
	}

	h.recordCRSignal(ctx, issue, p.PR.HTMLURL, p.PR.Head.SHA, "review_comment", p.Action, map[string]any{
		"comment_id": p.Comment.ID,
		"path":       p.Comment.Path,
		"line":       p.Comment.Line,
	})

	parsed := parseCRBody(p.Comment.Body)

	var linePG pgtype.Int4
	if p.Comment.Line > 0 {
		linePG = pgtypeInt4(int32(p.Comment.Line))
	}
	var sidePG pgtype.Text
	if p.Comment.Side != "" {
		sidePG = pgtypeText(p.Comment.Side)
	}

	params := db.UpsertReviewThreadParams{
		WorkspaceID:    pgtype.UUID{Bytes: issue.WorkspaceID.Bytes, Valid: true},
		IssueID:        pgtype.UUID{Bytes: issue.ID.Bytes, Valid: true},
		PrRepo:         binding.RepoFullName,
		PrNumber:       p.PR.Number,
		GhCommentID:    p.Comment.ID,
		GhThreadNodeID: pgtype.Text{Valid: false}, // populated later from review_thread payloads
		FilePath:       p.Comment.Path,
		Line:           linePG,
		Side:           sidePG,
		Severity:       parsed.Severity,
		SeverityBadge:  parsed.SeverityBadge,
		EffortBadge:    parsed.EffortBadge,
		AiPrompt:       parsed.AIPrompt,
		Title:          parsed.Title,
		Body:           p.Comment.Body,
		Url:            p.Comment.HTMLURL,
		AuthorLogin:    p.Comment.User.Login,
	}
	row, err := h.Queries.UpsertReviewThread(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("upsert review thread: %w", err)
	}

	// Mirror the thread into the comments timeline as a first-class
	// cr_review_comment row so the issue page renders one entry per CR
	// finding alongside human comments. Idempotent on review_thread_id.
	commentBody := renderCRComment(row, parsed)
	mirrored, cerr := h.Queries.UpsertCRReviewComment(ctx, db.UpsertCRReviewCommentParams{
		IssueID:        pgtype.UUID{Bytes: issue.ID.Bytes, Valid: true},
		WorkspaceID:    pgtype.UUID{Bytes: issue.WorkspaceID.Bytes, Valid: true},
		Content:        commentBody,
		ReviewThreadID: pgtype.UUID{Bytes: row.ID.Bytes, Valid: true},
	})
	if cerr != nil {
		// Comment-mirror is best-effort: log and continue. The thread
		// row is already saved; the UI's CR panel will show it via
		// /api/issues/{id}/review-threads even if the timeline mirror
		// fails. Future re-deliveries of the same comment heal the gap.
		slog.Warn("webhook: cr_review_comment mirror failed",
			"issue", uuidStr(issue.ID),
			"thread", uuidStr(row.ID),
			"error", cerr,
		)
	} else {
		// Publish a comment:created event so the frontend's timeline
		// + review-threads queries invalidate in real time. Without
		// this, the UI shows the new finding only on next refetch
		// (a TanStack staleTime tick or manual refresh).
		h.publishCRReviewCommentCreated(issue, mirrored, row)
	}

	return &dispatchResult{
		label: "review_comment_recorded",
		fields: map[string]any{
			"issue":         uuidStr(issue.ID),
			"gh_comment_id": p.Comment.ID,
			"severity":      row.Severity,
			"state":         row.State,
		},
	}, nil
}

func (h *WebhookHandler) handleCheckRun(ctx context.Context, payload map[string]json.RawMessage, binding db.WorkspaceRepoBinding) (*dispatchResult, error) {
	var p checkRunPayload
	if err := decode(payload, "action", &p.Action); err != nil {
		return nil, err
	}
	if err := decode(payload, "check_run", &p.CheckRun); err != nil {
		return nil, err
	}
	if !strings.EqualFold(p.CheckRun.App.Slug, "coderabbitai") &&
		!strings.EqualFold(p.CheckRun.App.Slug, binding.CrBotUsername) {
		return &dispatchResult{label: "noop", fields: map[string]any{"reason": "non-CR check_run"}}, nil
	}

	prNumber, err := h.resolvePRForCheckRun(ctx, binding, p.CheckRun)
	if err != nil || prNumber == 0 {
		return &dispatchResult{label: "noop", fields: map[string]any{"reason": "no PR for check_run", "error": fmt.Sprint(err)}}, nil
	}
	issue, found, err := h.resolveIssueByPR(ctx, binding.RepoFullName, prNumber)
	if err != nil {
		return nil, err
	}
	if !found {
		return &dispatchResult{label: "issue_not_found"}, nil
	}

	h.recordCRSignal(ctx, issue, p.CheckRun.HTMLURL, p.CheckRun.HeadSHA, "check_run", p.Action, map[string]any{
		"name":       p.CheckRun.Name,
		"status":     p.CheckRun.Status,
		"conclusion": p.CheckRun.Conclusion,
	})

	if p.Action != "completed" {
		return &dispatchResult{label: "check_run_recorded"}, nil
	}

	crRound := readPhaseStateCRRound(ctx, h.Queries, issue.ID)
	attempt, aerr := h.Queries.GetCRReviewAttempt(ctx, db.GetCRReviewAttemptParams{
		IssueID: issue.ID,
		CrRound: int32(crRound),
	})
	if aerr != nil && !errors.Is(aerr, pgx.ErrNoRows) {
		return nil, aerr
	}
	if aerr == nil && attempt.ClosedAt.Valid {
		return &dispatchResult{label: "check_run_recorded_attempt_already_closed"}, nil
	}

	switch p.CheckRun.Conclusion {
	case "skipped", "neutral":
		return h.txCloseAttemptAndSetStatus(ctx, issue, int32(crRound),
			"skipped", "check_run_"+p.CheckRun.Conclusion,
			StatusStaged, "check_run_skipped_to_staged",
			"review_passed")
	case "failure":
		reason := "check_run_failure"
		if p.CheckRun.Output.Title != "" {
			reason += ": " + p.CheckRun.Output.Title
		}
		return h.txCloseAttemptAndFailClosed(ctx, issue, int32(crRound), reason)
	}
	return &dispatchResult{label: "check_run_recorded"}, nil
}

func (h *WebhookHandler) handleIssueComment(ctx context.Context, payload map[string]json.RawMessage, binding db.WorkspaceRepoBinding) (*dispatchResult, error) {
	var p issueCommentPayload
	if err := decode(payload, "action", &p.Action); err != nil {
		return nil, err
	}
	if err := decode(payload, "issue", &p.Issue); err != nil {
		return nil, err
	}
	if err := decode(payload, "comment", &p.Comment); err != nil {
		return nil, err
	}
	if p.Action != "created" && p.Action != "edited" {
		return &dispatchResult{label: "noop", fields: map[string]any{"reason": "non-created/edited issue_comment"}}, nil
	}
	if p.Issue.PullRequest == nil || p.Issue.Number == 0 {
		return &dispatchResult{label: "noop", fields: map[string]any{"reason": "not a PR comment"}}, nil
	}
	if !strings.EqualFold(p.Comment.User.Login, binding.CrBotUsername) {
		return &dispatchResult{label: "noop", fields: map[string]any{"reason": "non-CR issue_comment"}}, nil
	}
	issue, found, err := h.resolveIssueByPR(ctx, binding.RepoFullName, p.Issue.Number)
	if err != nil {
		return nil, err
	}
	if !found {
		return &dispatchResult{label: "issue_not_found"}, nil
	}
	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", binding.RepoFullName, p.Issue.Number)
	if p.Issue.PullRequest.HTMLURL != "" {
		prURL = p.Issue.PullRequest.HTMLURL
	}
	h.recordCRSignal(ctx, issue, prURL, "", "issue_comment", p.Action, map[string]any{
		"comment_url": p.Comment.HTMLURL,
	})
	return &dispatchResult{label: "issue_comment_recorded"}, nil
}

func (h *WebhookHandler) recordCRSignal(ctx context.Context, issue db.Issue, prURL, headSHA string, signalKind, signalAction string, summary map[string]any) {
	crRound := readPhaseStateCRRound(ctx, h.Queries, issue.ID)
	attempt, err := h.Queries.UpsertCRReviewAttempt(ctx, db.UpsertCRReviewAttemptParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		CrRound:     int32(crRound),
		PrUrl:       prURL,
		HeadSha:     headSHA,
	})
	if err != nil {
		slog.Warn("recordCRSignal: upsert attempt failed", "issue", uuidStr(issue.ID), "kind", signalKind, "error", err)
		return
	}
	if err := h.Queries.RecordCRFirstSignal(ctx, db.RecordCRFirstSignalParams{
		IssueID:         issue.ID,
		CrRound:         int32(crRound),
		FirstSignalKind: pgtype.Text{String: signalKind, Valid: true},
	}); err != nil {
		slog.Warn("recordCRSignal: first signal write failed", "issue", uuidStr(issue.ID), "kind", signalKind, "error", err)
	}
	summaryJSON, _ := json.Marshal(summary)
	if err := h.Queries.InsertCRReviewSignal(ctx, db.InsertCRReviewSignalParams{
		AttemptID:      attempt.ID,
		SignalKind:     signalKind,
		SignalAction:   pgtype.Text{String: signalAction, Valid: signalAction != ""},
		PayloadSummary: summaryJSON,
	}); err != nil {
		slog.Warn("recordCRSignal: signal insert failed", "issue", uuidStr(issue.ID), "kind", signalKind, "error", err)
	}
}

func reviewThreadAuthoredByCR(ctx context.Context, q *db.Queries, thread reviewThreadInfo, crBot string) bool {
	for _, c := range thread.Comments {
		if c.ID == 0 {
			continue
		}
		local, err := q.GetReviewThreadByCommentID(ctx, c.ID)
		if err == nil && strings.EqualFold(local.AuthorLogin, crBot) {
			return true
		}
	}
	return false
}

func (h *WebhookHandler) txCloseAttemptAndSetStatus(ctx context.Context, issue db.Issue, crRound int32, outcome, outcomeReason, newStatus, label, activityKind string) (*dispatchResult, error) {
	prevStatus := issue.Status
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	closed, err := qtx.CloseCRReviewAttempt(ctx, db.CloseCRReviewAttemptParams{
		IssueID:       issue.ID,
		CrRound:       crRound,
		Outcome:       pgtype.Text{String: outcome, Valid: true},
		OutcomeReason: pgtype.Text{String: outcomeReason, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &dispatchResult{label: "check_run_attempt_already_closed"}, nil
		}
		return nil, err
	}
	updated, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issue.ID, Status: newStatus})
	if err != nil {
		return nil, err
	}
	if err := audit.WriteCRAttemptAuditComment(ctx, qtx, issue, closed); err != nil {
		return nil, err
	}
	details, _ := json.Marshal(map[string]any{
		"from": prevStatus, "to": newStatus, "reason": outcomeReason, "outcome": outcome, "cr_round": crRound,
	})
	if _, err := qtx.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     pgtype.UUID{Bytes: issue.ID.Bytes, Valid: true},
		ActorType:   pgtype.Text{String: "system", Valid: true},
		Action:      activityKind,
		Details:     details,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	h.Bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: uuidStr(issue.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"id": uuidStr(updated.ID), "status": updated.Status,
			"prev": prevStatus, "prev_status": prevStatus, "status_changed": true,
			"source": "cr_check_run", "src_event": outcomeReason,
		},
	})
	return &dispatchResult{label: label}, nil
}

func (h *WebhookHandler) txCloseAttemptAndFailClosed(ctx context.Context, issue db.Issue, crRound int32, reason string) (*dispatchResult, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.CloseCRReviewAttempt(ctx, db.CloseCRReviewAttemptParams{
		IssueID:       issue.ID,
		CrRound:       crRound,
		Outcome:       pgtype.Text{String: "failed", Valid: true},
		OutcomeReason: pgtype.Text{String: reason, Valid: true},
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &dispatchResult{label: "check_run_attempt_already_closed"}, nil
		}
		return nil, err
	}
	updated, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issue.ID, Status: StatusBlocked})
	if err != nil {
		return nil, err
	}
	body := fmt.Sprintf("<!-- sidecar-block -->\n\nreason: %s\n", reason)
	if _, err := qtx.CreateComment(ctx, db.CreateCommentParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		AuthorType: "system", AuthorID: pgtype.UUID{Valid: false},
		Content: body, Type: "system", ParentID: pgtype.UUID{Valid: false},
	}); err != nil {
		return nil, err
	}
	details, _ := json.Marshal(map[string]any{"from": issue.Status, "to": StatusBlocked, "reason": reason})
	if _, err := qtx.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     pgtype.UUID{Bytes: issue.ID.Bytes, Valid: true},
		ActorType:   pgtype.Text{String: "system", Valid: true},
		Action:      "review_blocked",
		Details:     details,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	h.Bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: uuidStr(issue.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"id": uuidStr(updated.ID), "status": updated.Status,
			"prev": issue.Status, "prev_status": issue.Status, "status_changed": true,
			"source": "cr_check_run", "src_event": reason,
		},
	})
	return &dispatchResult{label: "failed_closed_to_blocked", fields: map[string]any{"reason": reason}}, nil
}

func (h *WebhookHandler) resolvePRForCheckRun(ctx context.Context, binding db.WorkspaceRepoBinding, cr checkRunInfo) (int32, error) {
	if len(cr.PullRequests) > 0 && cr.PullRequests[0].Number > 0 {
		return cr.PullRequests[0].Number, nil
	}
	if cr.HeadSHA == "" {
		return 0, nil
	}
	owner, repo, ok := splitRepo(binding.RepoFullName)
	if !ok {
		return 0, fmt.Errorf("invalid repo full name: %s", binding.RepoFullName)
	}
	var c PRReviewClient
	if h.NewClient != nil {
		c = h.NewClient(binding.InstallationID)
	} else {
		c = NewGitHubAPIClient(h.Auth, binding.InstallationID)
	}
	prs, err := c.ListPRsForCommit(ctx, owner, repo, cr.HeadSHA)
	if err != nil {
		return 0, err
	}
	for _, pr := range prs {
		if pr.State == "open" {
			return int32(pr.Number), nil
		}
	}
	return 0, nil
}

// renderCRComment formats a CodeRabbit review-thread row as a markdown
// comment body suitable for the issue timeline. The format is
// intentionally compact — badges as bold-italic chips on the first line,
// the parsed title (or fallback to the thread.title), the file:line
// reference, and the AI prompt under a collapsible details block. The
// raw CR body is NOT inlined; users can deep-link to the GitHub thread
// for the full payload (analysis, tools, patches).
func renderCRComment(t db.IssueReviewThread, p crParsed) string {
	var b strings.Builder
	// Badge chips (one per non-unknown field).
	chips := []string{}
	if p.Severity != "" && p.Severity != "unknown" {
		chips = append(chips, "**"+strings.Title(p.Severity)+"**") //nolint:staticcheck // Title is fine for ASCII labels
	}
	if p.SeverityBadge != "" && p.SeverityBadge != "unknown" {
		chips = append(chips, "**"+p.SeverityBadge+"**")
	}
	if p.EffortBadge != "" && p.EffortBadge != "unknown" {
		chips = append(chips, "_"+p.EffortBadge+"_")
	}
	if len(chips) > 0 {
		b.WriteString(strings.Join(chips, " · "))
		b.WriteString("\n\n")
	}
	title := p.Title
	if title == "" {
		title = t.Title
	}
	if title != "" {
		b.WriteString("**" + title + "**\n\n")
	}
	if t.FilePath != "" {
		ref := t.FilePath
		if t.Line.Valid && t.Line.Int32 > 0 {
			ref = fmt.Sprintf("%s:L%d", t.FilePath, t.Line.Int32)
		}
		if t.Url != "" {
			b.WriteString(fmt.Sprintf("[`%s`](%s)\n\n", ref, t.Url))
		} else {
			b.WriteString("`" + ref + "`\n\n")
		}
	}
	if p.AIPrompt != "" {
		b.WriteString("<details>\n<summary>🤖 Prompt for AI Agents</summary>\n\n```\n")
		b.WriteString(p.AIPrompt)
		b.WriteString("\n```\n\n</details>\n")
	}
	return b.String()
}

// crParsed captures the structured fields we extract from a CodeRabbit
// review-comment body. Fields default to "unknown" / "" when absent so
// downstream code never has to nil-check.
type crParsed struct {
	Severity      string // issue | refactor | nitpick | suggestion | unknown
	SeverityBadge string // Critical | Major | Minor | Trivial | Blocker | unknown
	EffortBadge   string // Quick win | Heavy lift | Poor tradeoff | Low value | unknown
	Title         string // first **bold** line, ~140 chars
	AIPrompt      string // verbatim contents of the fenced block under "🤖 Prompt for AI Agents"
}

// parseCRBody extracts the structured fields from a CodeRabbit review-comment
// body. CR's standard layout (2026 format):
//
//	_<severity_emoji> <severity_text>_ | _<severity_badge_emoji> <text>_ | _<effort_badge_emoji> <text>_
//
//	**<title>.**
//
//	<body description...>
//
//	<details>
//	<summary>🤖 Prompt for AI Agents</summary>
//
//	```
//	<verbatim AI prompt>
//	```
//
//	</details>
//
// The third badge (effort) is optional. Fields are case-insensitive against
// CR's vocabulary; the canonical written form is preserved (e.g. "Major"
// not "major") so the UI renders consistently.
func parseCRBody(body string) crParsed {
	out := crParsed{Severity: "unknown", SeverityBadge: "unknown", EffortBadge: "unknown"}

	// 1. Badges: first non-empty line, pipe-separated underscore-italics.
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Must look like a badge line.
		if !crBadgeLineRE.MatchString(line) {
			break
		}
		for _, seg := range strings.Split(line, "|") {
			tag := strings.Trim(strings.TrimSpace(seg), "_")
			if tag == "" {
				continue
			}
			lower := strings.ToLower(tag)
			switch {
			case strings.Contains(lower, "potential issue"):
				out.Severity = "issue"
			case strings.Contains(lower, "refactor suggestion"), strings.Contains(lower, "refactor:"):
				out.Severity = "refactor"
			case strings.Contains(lower, "nitpick"), strings.Contains(lower, "nit:"):
				out.Severity = "nitpick"
			case strings.Contains(lower, "verification agent"):
				out.Severity = "suggestion"
			case strings.Contains(lower, "suggestion"):
				if out.Severity == "unknown" {
					out.Severity = "suggestion"
				}
			}
			switch {
			case strings.Contains(lower, "critical"):
				out.SeverityBadge = "Critical"
			case strings.Contains(lower, "blocker"):
				out.SeverityBadge = "Blocker"
			case strings.Contains(lower, "major"):
				out.SeverityBadge = "Major"
			case strings.Contains(lower, "minor"):
				out.SeverityBadge = "Minor"
			case strings.Contains(lower, "trivial"):
				out.SeverityBadge = "Trivial"
			}
			switch {
			case strings.Contains(lower, "quick win"):
				out.EffortBadge = "Quick win"
			case strings.Contains(lower, "heavy lift"):
				out.EffortBadge = "Heavy lift"
			case strings.Contains(lower, "poor tradeoff"):
				out.EffortBadge = "Poor tradeoff"
			case strings.Contains(lower, "low value"):
				out.EffortBadge = "Low value"
			}
		}
		break
	}

	// 2. Title: first **bold** line outside any <details> block, with a
	// non-empty/non-badge fallback. CR often wraps an "Analysis chain" or
	// "Tools" section in <details> ahead of the actual title — we walk
	// those skip-blocks rather than picking up `<details>` as the heading.
	depth := 0
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "<details") {
			depth++
			continue
		}
		if strings.HasPrefix(line, "</details") {
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 {
			continue
		}
		if crBadgeLineRE.MatchString(line) {
			continue
		}
		if m := crBoldTitleRE.FindStringSubmatch(line); m != nil {
			out.Title = strings.TrimSpace(m[1])
			break
		}
		line = strings.TrimLeft(line, "#*-> \t")
		if line == "" {
			continue
		}
		out.Title = line
		break
	}
	if len(out.Title) > 140 {
		out.Title = out.Title[:140]
	}

	// 3. AI prompt: fenced block inside the AI-agents <details>.
	if m := crAIPromptRE.FindStringSubmatch(body); m != nil {
		out.AIPrompt = strings.TrimSpace(m[1])
	}

	return out
}

var (
	// Underscore-italic segments separated by pipes:
	//   _⚠️ Potential issue_ | _🟠 Major_ | _⚡ Quick win_
	crBadgeLineRE = regexp.MustCompile(`^_[^_\n]+_(\s*\|\s*_[^_\n]+_)*\s*$`)

	// **Title text.**  (with optional trailing punctuation captured)
	crBoldTitleRE = regexp.MustCompile(`^\*\*(.+?)\*\*\s*$`)

	// <details><summary>🤖 Prompt for AI Agents</summary> ... ```<content>``` ... </details>
	// Non-greedy across newlines (?s flag); capture group is the fenced body.
	crAIPromptRE = regexp.MustCompile("(?s)<summary>\\s*🤖\\s*Prompt for AI Agents\\s*</summary>.*?```[a-zA-Z]*\\n(.*?)\\n```")
)

// ---------------------------------------------------------------------------
// Issue resolution
// ---------------------------------------------------------------------------

func (h *WebhookHandler) resolveIssueForPR(ctx context.Context, binding db.WorkspaceRepoBinding, pr prInfo) (db.Issue, bool, error) {
	// First try by stored PR linkage (PR was opened previously and we already
	// recorded pr_repo/pr_number).
	if issue, found, err := h.resolveIssueByPR(ctx, binding.RepoFullName, pr.Number); err != nil {
		return db.Issue{}, false, err
	} else if found {
		return issue, true, nil
	}

	// Fall back to identifier extraction from branch/title/body.
	id, ok := ExtractIdentifier(pr.HeadRef, pr.Body, pr.Title)
	if !ok {
		return db.Issue{}, false, nil
	}
	issue, err := h.Queries.GetIssueByIdentifier(ctx, db.GetIssueByIdentifierParams{
		WorkspaceID: binding.WorkspaceID,
		IssuePrefix: id.Prefix,
		Number:      id.Number,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Issue{}, false, nil
		}
		return db.Issue{}, false, err
	}
	return issue, true, nil
}

func (h *WebhookHandler) resolveIssueByPR(ctx context.Context, repo string, number int32) (db.Issue, bool, error) {
	issue, err := h.Queries.GetIssueByPR(ctx, db.GetIssueByPRParams{
		PrRepo:   pgtypeText(repo),
		PrNumber: pgtypeInt4(number),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Issue{}, false, nil
		}
		return db.Issue{}, false, err
	}
	return issue, true, nil
}

// ---------------------------------------------------------------------------
// Apply Decision
// ---------------------------------------------------------------------------

func (h *WebhookHandler) applyDecision(ctx context.Context, issue db.Issue, dec Decision, pr prInfo, binding db.WorkspaceRepoBinding, srcEvent string) (*dispatchResult, error) {
	prevStatus := issue.Status
	var updated db.Issue
	var err error
	var tx pgx.Tx

	switch dec.Action {
	case ActionLinkPR:
		updated, err = h.Queries.SetIssuePR(ctx, db.SetIssuePRParams{
			ID:       issue.ID,
			PrUrl:    pgtypeText(pr.HTMLURL),
			PrNumber: pgtypeInt4(pr.Number),
			PrRepo:   pgtypeText(binding.RepoFullName),
		})
		if err == nil && dec.NewStatus != prevStatus {
			updated, err = h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID:     issue.ID,
				Status: dec.NewStatus,
			})
		}
	case ActionSetStatus:
		updated, err = h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:     issue.ID,
			Status: dec.NewStatus,
		})
	case ActionRecordPendingApproval:
		crRound := readPhaseStateCRRound(ctx, h.Queries, issue.ID)
		tx, err = h.TxStarter.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		qtx := h.Queries.WithTx(tx)
		if _, err = qtx.UpsertCRReviewAttempt(ctx, db.UpsertCRReviewAttemptParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			CrRound:     int32(crRound),
			PrUrl:       pr.HTMLURL,
			HeadSha:     pr.Head.SHA,
		}); err != nil {
			return nil, err
		}
		if _, err = qtx.RecordCRWrappingReview(ctx, db.RecordCRWrappingReviewParams{
			IssueID:       issue.ID,
			CrRound:       int32(crRound),
			ReviewState:   pgtype.Text{String: string(dec.ReviewStateRecord), Valid: true},
			FindingsCount: int32(dec.FindingsCount),
		}); err != nil {
			return nil, err
		}
		details, _ := json.Marshal(map[string]any{
			"status":    issue.Status,
			"phase":     "commented_clean_pending",
			"pr_url":    pr.HTMLURL,
			"pr_number": pr.Number,
			"pr_repo":   binding.RepoFullName,
			"reason":    "commented_clean_pending_settle",
			"cr_round":  crRound,
		})
		if _, err = qtx.CreateActivity(ctx, db.CreateActivityParams{
			WorkspaceID: issue.WorkspaceID,
			IssueID:     pgtype.UUID{Bytes: issue.ID.Bytes, Valid: true},
			ActorType:   pgtype.Text{String: "system", Valid: true},
			Action:      dec.ActivityKind,
			Details:     details,
		}); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		// No status changed here, so do not publish the normal issue:updated
		// event. PR 3 can add a dedicated activity event for CR Activity refresh.
		return &dispatchResult{label: "pending_approval_recorded", fields: map[string]any{"issue": uuidStr(issue.ID)}}, nil
	case ActionSetStatusAndCloseAttempt:
		crRound := readPhaseStateCRRound(ctx, h.Queries, issue.ID)
		tx, err = h.TxStarter.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		qtx := h.Queries.WithTx(tx)
		if _, err = qtx.UpsertCRReviewAttempt(ctx, db.UpsertCRReviewAttemptParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			CrRound:     int32(crRound),
			PrUrl:       pr.HTMLURL,
			HeadSha:     pr.Head.SHA,
		}); err != nil {
			return nil, err
		}
		if _, err = qtx.RecordCRWrappingReview(ctx, db.RecordCRWrappingReviewParams{
			IssueID:       issue.ID,
			CrRound:       int32(crRound),
			ReviewState:   pgtype.Text{String: string(dec.ReviewStateRecord), Valid: true},
			FindingsCount: int32(dec.FindingsCount),
		}); err != nil {
			return nil, err
		}
		closed, closeErr := qtx.CloseCRReviewAttempt(ctx, db.CloseCRReviewAttemptParams{
			IssueID:       issue.ID,
			CrRound:       int32(crRound),
			Outcome:       pgtype.Text{String: dec.AttemptOutcome, Valid: true},
			OutcomeReason: pgtype.Text{String: dec.AttemptReason, Valid: true},
		})
		if closeErr != nil {
			err = closeErr
			return nil, err
		}
		updated, err = qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:     issue.ID,
			Status: dec.NewStatus,
		})
		if err != nil {
			return nil, err
		}
		if err = audit.WriteCRAttemptAuditComment(ctx, qtx, issue, closed); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
	}
	if err != nil {
		return nil, fmt.Errorf("apply decision: %w", err)
	}

	// Write activity row.
	details, _ := json.Marshal(map[string]any{
		"from":      prevStatus,
		"to":        dec.NewStatus,
		"pr_url":    pr.HTMLURL,
		"pr_number": pr.Number,
		"pr_repo":   binding.RepoFullName,
	})
	_, err = h.Queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: updated.WorkspaceID,
		IssueID:     pgtype.UUID{Bytes: updated.ID.Bytes, Valid: true},
		ActorType:   pgtype.Text{String: "system", Valid: true},
		// ActorID intentionally NULL — webhooks have no user actor.
		Action:  dec.ActivityKind,
		Details: details,
	})
	if err != nil {
		// Activity write is best-effort; log and continue.
		slog.Error("webhook: activity write failed", "issue", updated.ID, "kind", dec.ActivityKind, "error", err)
	}

	// Publish bus events for WS broadcast.
	//
	// We include the full issue object under "issue" (mirroring handler.publish's
	// shape for protocol.EventIssueUpdated) so the frontend's global WS handler
	// can hydrate query caches naturally — same path a UI-driven status change
	// would take. Flat fields are kept for any consumer that prefers them.
	wsID := uuidStr(updated.WorkspaceID)
	issueResp, prefix, respErr := h.buildIssueResponse(ctx, updated)
	if respErr != nil {
		slog.Warn("webhook: failed to build issue response for WS payload", "issue", uuidStr(updated.ID), "error", respErr)
	}
	_ = prefix
	h.Bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsID,
		ActorType:   "system",
		Payload: map[string]any{
			"issue":          issueResp,
			"id":             uuidStr(updated.ID),
			"status":         updated.Status,
			"prev":           prevStatus,
			"status_changed": updated.Status != prevStatus,
			"prev_status":    prevStatus,
			"source":         "github_webhook",
			"src_event":      srcEvent,
			"pr_number":      pr.Number,
			"pr_url":         pr.HTMLURL,
		},
	})

	return &dispatchResult{
		label: "applied",
		fields: map[string]any{
			"issue":         uuidStr(updated.ID),
			"from":          prevStatus,
			"to":            dec.NewStatus,
			"activity_kind": dec.ActivityKind,
		},
	}, nil
}

func (h *WebhookHandler) failClosedToBlocked(ctx context.Context, issue db.Issue, pr prInfo, binding db.WorkspaceRepoBinding, reason string) (*dispatchResult, error) {
	if h.TxStarter == nil {
		return nil, errors.New("fail-closed requires TxStarter")
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	crRound := readPhaseStateCRRound(ctx, h.Queries, issue.ID)
	if _, err := qtx.UpsertCRReviewAttempt(ctx, db.UpsertCRReviewAttemptParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		CrRound:     int32(crRound),
		PrUrl:       pr.HTMLURL,
		HeadSha:     pr.Head.SHA,
	}); err != nil {
		return nil, fmt.Errorf("fail-closed upsert attempt: %w", err)
	}
	if _, err := qtx.CloseCRReviewAttempt(ctx, db.CloseCRReviewAttemptParams{
		IssueID:       issue.ID,
		CrRound:       int32(crRound),
		Outcome:       pgtype.Text{String: "failed", Valid: true},
		OutcomeReason: pgtype.Text{String: reason, Valid: true},
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &dispatchResult{
				label:  "fail_closed_attempt_already_closed",
				fields: map[string]any{"reason": reason},
			}, nil
		}
		return nil, fmt.Errorf("fail-closed close attempt: %w", err)
	}
	updated, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issue.ID,
		Status: StatusBlocked,
	})
	if err != nil {
		return nil, fmt.Errorf("fail-closed status update: %w", err)
	}
	body := fmt.Sprintf("<!-- sidecar-block -->\n\nreason: %s\n", reason)
	if _, err := qtx.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: false},
		Content:     body,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	}); err != nil {
		return nil, fmt.Errorf("fail-closed comment: %w", err)
	}
	details, _ := json.Marshal(map[string]any{
		"from": issue.Status, "to": StatusBlocked, "reason": reason,
	})
	if _, err := qtx.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     pgtype.UUID{Bytes: issue.ID.Bytes, Valid: true},
		ActorType:   pgtype.Text{String: "system", Valid: true},
		Action:      "review_blocked",
		Details:     details,
	}); err != nil {
		return nil, fmt.Errorf("fail-closed activity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	h.Bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: uuidStr(issue.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"id":             uuidStr(issue.ID),
			"status":         updated.Status,
			"prev":           issue.Status,
			"prev_status":    issue.Status,
			"status_changed": true,
			"source":         "cr_fail_closed",
			"src_event":      reason,
		},
	})
	return &dispatchResult{label: "failed_closed_to_blocked", fields: map[string]any{"reason": reason}}, nil
}

func readPhaseStateCRRound(ctx context.Context, q *db.Queries, issueID pgtype.UUID) int {
	iss, err := q.GetIssue(ctx, issueID)
	if err != nil || len(iss.PhaseState) == 0 {
		return 0
	}
	var s struct {
		CRRound int `json:"cr_round"`
	}
	if err := json.Unmarshal(iss.PhaseState, &s); err != nil || s.CRRound < 0 {
		return 0
	}
	return s.CRRound
}

// ---------------------------------------------------------------------------
// CR predicate
// ---------------------------------------------------------------------------

// predicate computes the two CR-thread booleans + the local unresolved count
// for a given issue's PR.
//
// noOpenChanges (no open CHANGES_REQUESTED review) is computed from GitHub's
// REST reviews API — that's the only place the latest review state lives.
//
// noUnresolved + unresolvedCount come from our local issue_review_thread
// table, which is the source of truth for thread state inside Multica. The
// table is kept in sync with GitHub via the pull_request_review_thread
// resolved/unresolved webhook deliveries (see handleReviewThread). Reading
// locally lets us reflect resolutions made by the dev agent in the fixing
// loop the moment they're written, without a GraphQL round-trip, and lets
// the state machine drive coderabbit → resolving on stale unresolved counts
// even when CR's review state is COMMENTED rather than CHANGES_REQUESTED.
func (h *WebhookHandler) predicate(ctx context.Context, binding db.WorkspaceRepoBinding, prNumber int32, issueID pgtype.UUID) (noOpenChanges, noUnresolved bool, unresolvedCount int, err error) {
	owner, repo, ok := splitRepo(binding.RepoFullName)
	if !ok {
		return false, false, 0, fmt.Errorf("invalid repo: %s", binding.RepoFullName)
	}
	var c PRReviewClient
	if h.NewClient != nil {
		c = h.NewClient(binding.InstallationID)
	} else {
		c = NewGitHubAPIClient(h.Auth, binding.InstallationID)
	}

	// noOpenChanges: walk reviews from GitHub.
	reviews, rerr := c.ListReviews(ctx, owner, repo, int(prNumber))
	if rerr != nil {
		return false, false, 0, fmt.Errorf("list reviews: %w", rerr)
	}
	noOpenChanges = true
	var latestCRState string
	for _, r := range reviews {
		if !equalLogin(r.User.Login, binding.CrBotUsername) {
			continue
		}
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
			latestCRState = r.State
		}
	}
	if latestCRState == "CHANGES_REQUESTED" {
		noOpenChanges = false
	}

	// noUnresolved + count: read from our local mirror.
	count, cerr := h.Queries.CountUnresolvedReviewThreadsByIssue(ctx, issueID)
	if cerr != nil {
		return false, false, 0, fmt.Errorf("count unresolved review threads: %w", cerr)
	}
	unresolvedCount = int(count)
	noUnresolved = unresolvedCount == 0
	return noOpenChanges, noUnresolved, unresolvedCount, nil
}

// bulkMirrorReviewComments fetches the inline comments belonging to a single
// CR review submission and upserts each into issue_review_thread. It runs
// before predicate() in handleReview so LocalUnresolvedThreadCount reflects
// the full set of findings rather than the racing count seen at the moment
// the review webhook arrives.
//
// Skips non-CR-authored comments defensively even though the endpoint is
// scoped to a single review (CR's review can technically only contain its
// own author's comments, but we filter anyway to keep the local mirror's
// invariant — only CR threads — explicit).
//
// Does NOT mirror into cr_review_comment (the timeline) or publish bus
// events; that work stays in handleReviewComment so we don't double-publish
// when the per-comment webhooks arrive afterward.
func (h *WebhookHandler) bulkMirrorReviewComments(ctx context.Context, binding db.WorkspaceRepoBinding, issue db.Issue, prNumber int32, reviewID int64) error {
	owner, repo, ok := splitRepo(binding.RepoFullName)
	if !ok {
		return fmt.Errorf("invalid repo: %s", binding.RepoFullName)
	}
	var c PRReviewClient
	if h.NewClient != nil {
		c = h.NewClient(binding.InstallationID)
	} else {
		c = NewGitHubAPIClient(h.Auth, binding.InstallationID)
	}
	comments, err := c.ListReviewComments(ctx, owner, repo, int(prNumber), reviewID)
	if err != nil {
		return fmt.Errorf("list review comments: %w", err)
	}
	for _, cm := range comments {
		if !strings.EqualFold(cm.User.Login, binding.CrBotUsername) {
			continue
		}
		parsed := parseCRBody(cm.Body)
		var linePG pgtype.Int4
		if cm.Line > 0 {
			linePG = pgtypeInt4(int32(cm.Line))
		}
		var sidePG pgtype.Text
		if cm.Side != "" {
			sidePG = pgtypeText(cm.Side)
		}
		if _, uerr := h.Queries.UpsertReviewThread(ctx, db.UpsertReviewThreadParams{
			WorkspaceID:    pgtype.UUID{Bytes: issue.WorkspaceID.Bytes, Valid: true},
			IssueID:        pgtype.UUID{Bytes: issue.ID.Bytes, Valid: true},
			PrRepo:         binding.RepoFullName,
			PrNumber:       prNumber,
			GhCommentID:    cm.ID,
			GhThreadNodeID: pgtype.Text{Valid: false}, // populated later from review_thread payloads
			FilePath:       cm.Path,
			Line:           linePG,
			Side:           sidePG,
			Severity:       parsed.Severity,
			SeverityBadge:  parsed.SeverityBadge,
			EffortBadge:    parsed.EffortBadge,
			AiPrompt:       parsed.AIPrompt,
			Title:          parsed.Title,
			Body:           cm.Body,
			Url:            cm.HTMLURL,
			AuthorLogin:    cm.User.Login,
		}); uerr != nil {
			return fmt.Errorf("upsert review thread (gh_comment_id=%d): %w", cm.ID, uerr)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type prInfo struct {
	Number    int32  `json:"number"`
	HTMLURL   string `json:"html_url"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Merged    bool   `json:"merged"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
	Head      struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	HeadRef string `json:"-"`
}

type senderInfo struct {
	Login string `json:"login"`
}

type prPayload struct {
	Action string     `json:"action"`
	PR     prInfo     `json:"pull_request"`
	Sender senderInfo `json:"sender"`
}

type reviewPayload struct {
	Action string `json:"action"`
	Review struct {
		ID    int64  `json:"id"`
		State string `json:"state"`
		User  struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"review"`
	PR     prInfo     `json:"pull_request"`
	Sender senderInfo `json:"sender"`
}

type reviewThreadPayload struct {
	Action string           `json:"action"`
	PR     prInfo           `json:"pull_request"`
	Sender senderInfo       `json:"sender"`
	Thread reviewThreadInfo `json:"thread"`
}

// reviewThreadInfo carries the GraphQL node_id and per-comment numeric ids
// that GitHub includes on pull_request_review_thread payloads. Both keys
// help us locate the matching issue_review_thread row(s).
type reviewThreadInfo struct {
	NodeID   string `json:"node_id"`
	Comments []struct {
		ID int64 `json:"id"`
	} `json:"comments"`
}

// isCRAutoAckBody returns true when a CodeRabbit review comment body is one
// of CR's automatic acknowledgement replies (e.g. "Skipped: comment is from
// another GitHub bot."). These carry a unique HTML marker that legitimate CR
// findings never include. Dropping them at ingest time prevents the resolver
// from chasing CR's reply to its own fixer-reply post.
func isCRAutoAckBody(body string) bool {
	return strings.Contains(body, "<!-- This is an auto-generated reply by CodeRabbit -->")
}

// reviewCommentPayload mirrors the pull_request_review_comment event. We
// only use the fields needed to upsert one issue_review_thread row.
type reviewCommentPayload struct {
	Action  string `json:"action"`
	Comment struct {
		ID      int64  `json:"id"`
		Body    string `json:"body"`
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Side    string `json:"side"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		PullRequestReviewID int64 `json:"pull_request_review_id"`
	} `json:"comment"`
	PR     prInfo     `json:"pull_request"`
	Sender senderInfo `json:"sender"`
}

type checkRunPayload struct {
	Action   string       `json:"action"`
	CheckRun checkRunInfo `json:"check_run"`
}

type checkRunInfo struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSHA    string `json:"head_sha"`
	HTMLURL    string `json:"html_url"`
	App        struct {
		Slug string `json:"slug"`
	} `json:"app"`
	Output struct {
		Title string `json:"title"`
	} `json:"output"`
	PullRequests []struct {
		Number int32 `json:"number"`
	} `json:"pull_requests"`
}

type issueCommentPayload struct {
	Action string `json:"action"`
	Issue  struct {
		Number      int32 `json:"number"`
		PullRequest *struct {
			HTMLURL string `json:"html_url"`
		} `json:"pull_request"`
	} `json:"issue"`
	Comment struct {
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
}

// secondsSincePROpened returns the number of seconds between createdAt
// (the PR's created_at timestamp from GitHub) and now. Returns 0 when the
// timestamp is missing or unparseable, which disables the cooldown check
// in the state machine.
func secondsSincePROpened(createdAt string) int64 {
	if createdAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return 0
	}
	delta := time.Since(t).Seconds()
	if delta < 0 {
		return 0
	}
	return int64(delta)
}

// decode unmarshals payload[key] into out. We use a custom Unmarshaler shim
// for prInfo so HeadRef gets populated from .head.ref.
func decode(payload map[string]json.RawMessage, key string, out any) error {
	raw, ok := payload[key]
	if !ok {
		return fmt.Errorf("payload missing %q", key)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %q: %w", key, err)
	}
	if pr, ok := out.(*prInfo); ok {
		pr.HeadRef = pr.Head.Ref
	}
	return nil
}

func extractRepo(payload map[string]json.RawMessage) (string, error) {
	raw, ok := payload["repository"]
	if !ok {
		return "", errors.New("payload missing repository")
	}
	var r struct {
		FullName string `json:"full_name"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode repository: %w", err)
	}
	if r.FullName == "" {
		return "", errors.New("empty repository.full_name")
	}
	return r.FullName, nil
}

func splitRepo(full string) (string, string, bool) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// verifySignature does constant-time comparison against the hex-encoded
// HMAC-SHA256 of the body using secret as the key. The header format is
// "sha256=<hex>".
func verifySignature(header, secret string, body []byte) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	expected, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(expected, mac.Sum(nil))
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}

func writeOK(w http.ResponseWriter, label string, fields map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	out := map[string]any{"ok": true, "action": label}
	for k, v := range fields {
		out[k] = v
	}
	_ = json.NewEncoder(w).Encode(out)
}

func pgtypeText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func pgtypeInt4(n int32) pgtype.Int4 {
	return pgtype.Int4{Int32: n, Valid: true}
}

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Quiet "unused" for bytes.Buffer if linters complain — we may use it later
// for streaming payloads.
var _ = bytes.NewReader
var _ = io.Discard

// publishCRReviewCommentCreated emits a comment:created bus event so the
// frontend's WS handler invalidates the timeline + review-threads queries
// the moment a CR finding lands. Without this, the new finding only surfaces
// on the next stale-time tick or a manual refresh.
//
// Payload shape mirrors internal/handler.commentToResponse output JSON tags
// (we can't import that struct without creating a circular dep).
func (h *WebhookHandler) publishCRReviewCommentCreated(issue db.Issue, mirrored db.Comment, thread db.IssueReviewThread) {
	if h.Bus == nil {
		return
	}
	wsID := uuidStr(issue.WorkspaceID)
	commentPayload := map[string]any{
		"id":               uuidStr(mirrored.ID),
		"issue_id":         uuidStr(mirrored.IssueID),
		"author_type":      mirrored.AuthorType,
		"author_id":        uuidStrOrEmpty(mirrored.AuthorID),
		"content":          mirrored.Content,
		"type":             mirrored.Type,
		"parent_id":        nil,
		"review_thread_id": uuidStr(thread.ID),
		"created_at":       mirrored.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05.000Z"),
		"updated_at":       mirrored.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05.000Z"),
		"reactions":        []any{},
		"attachments":      []any{},
	}
	h.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: wsID,
		ActorType:   "system",
		Payload: map[string]any{
			"comment":      commentPayload,
			"issue_title":  issue.Title,
			"issue_status": issue.Status,
			"source":       "github_webhook",
			"src_event":    "pull_request_review_comment",
		},
	})
}

// uuidStrOrEmpty returns "" for an invalid UUID; helps when a column is
// nullable and we want a JSON null rather than a zero string.
func uuidStrOrEmpty(u pgtype.UUID) any {
	if !u.Valid {
		return nil
	}
	return uuidStr(u)
}

// buildIssueResponse constructs a payload matching internal/handler.IssueResponse
// JSON shape. Defined locally (not imported) to avoid a circular dependency
// between the github integration package and the handler package.
//
// Returns (response, prefix, error). The response is intentionally a plain
// map so the json encoder shapes it exactly like the handler's struct.
func (h *WebhookHandler) buildIssueResponse(ctx context.Context, i db.Issue) (map[string]any, string, error) {
	ws, err := h.Queries.GetWorkspace(ctx, i.WorkspaceID)
	if err != nil {
		return nil, "", err
	}
	prefix := ws.IssuePrefix
	identifier := prefix + "-" + strconv.Itoa(int(i.Number))

	var description any
	if i.Description.Valid {
		description = i.Description.String
	}
	var assigneeType any
	if i.AssigneeType.Valid {
		assigneeType = i.AssigneeType.String
	}
	var assigneeID any
	if i.AssigneeID.Valid {
		assigneeID = uuidStr(i.AssigneeID)
	}
	var parentID any
	if i.ParentIssueID.Valid {
		parentID = uuidStr(i.ParentIssueID)
	}
	var projectID any
	if i.ProjectID.Valid {
		projectID = uuidStr(i.ProjectID)
	}
	var dueDate any
	if i.DueDate.Valid {
		dueDate = i.DueDate.Time.UTC().Format("2006-01-02T15:04:05.000Z")
	}

	var prURL any
	if i.PrUrl.Valid {
		prURL = i.PrUrl.String
	}
	var prNumber any
	if i.PrNumber.Valid {
		prNumber = i.PrNumber.Int32
	}
	var prRepo any
	if i.PrRepo.Valid {
		prRepo = i.PrRepo.String
	}

	resp := map[string]any{
		"id":              uuidStr(i.ID),
		"workspace_id":    uuidStr(i.WorkspaceID),
		"number":          i.Number,
		"identifier":      identifier,
		"title":           i.Title,
		"description":     description,
		"status":          i.Status,
		"priority":        i.Priority,
		"assignee_type":   assigneeType,
		"assignee_id":     assigneeID,
		"creator_type":    i.CreatorType,
		"creator_id":      uuidStr(i.CreatorID),
		"parent_issue_id": parentID,
		"project_id":      projectID,
		"position":        i.Position,
		"due_date":        dueDate,
		"created_at":      i.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05.000Z"),
		"updated_at":      i.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05.000Z"),
		"pr_url":          prURL,
		"pr_number":       prNumber,
		"pr_repo":         prRepo,
	}
	if len(i.PhaseState) > 0 {
		resp["phase_state"] = json.RawMessage(i.PhaseState)
	}
	return resp, prefix, nil
}
