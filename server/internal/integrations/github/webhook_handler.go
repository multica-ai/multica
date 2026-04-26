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
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WebhookHandler holds the dependencies needed to process GitHub webhook
// deliveries.
type WebhookHandler struct {
	Queries *db.Queries
	Bus     *events.Bus
	Auth    *AppAuth

	// Secret is the App-level webhook secret loaded from
	// GITHUB_APP_WEBHOOK_SECRET. Empty disables HMAC verification — only
	// safe in tests.
	Secret string

	// NewClient overrides client construction in tests.
	NewClient func(installationID int64) PRReviewClient
}

// NewWebhookHandlerFromEnv constructs the handler using GITHUB_APP_*
// environment variables.
func NewWebhookHandlerFromEnv(queries *db.Queries, bus *events.Bus) (*WebhookHandler, error) {
	auth, err := NewAppAuthFromEnv()
	if err != nil {
		return nil, err
	}
	secret := os.Getenv("GITHUB_APP_WEBHOOK_SECRET")
	if secret == "" {
		return nil, errors.New("GITHUB_APP_WEBHOOK_SECRET must be set")
	}
	return &WebhookHandler{
		Queries: queries,
		Bus:     bus,
		Auth:    auth,
		Secret:  secret,
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

	// We only care about three event types. Everything else is a fast 200.
	relevant := eventType == "pull_request" || eventType == "pull_request_review" || eventType == "pull_request_review_thread"
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
		Kind:        EventKindPR,
		IssueStatus: issue.Status,
		PRAction:    PRAction(p.Action),
		Merged:      p.PR.Merged,
	}
	dec := Decide(in)
	if dec.Action == ActionNoop {
		return &dispatchResult{label: "noop", fields: map[string]any{"issue": issue.ID.String(), "current": issue.Status}}, nil
	}
	return h.applyDecision(ctx, issue, dec, p.PR, binding, "pull_request")
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
	if p.Action != "submitted" {
		return &dispatchResult{label: "noop", fields: map[string]any{"reason": "non-submitted review action"}}, nil
	}

	issue, found, err := h.resolveIssueByPR(ctx, binding.RepoFullName, p.PR.Number)
	if err != nil {
		return nil, err
	}
	if !found {
		return &dispatchResult{label: "issue_not_found", fields: map[string]any{"pr_number": p.PR.Number}}, nil
	}

	noOpenChanges, noUnresolved, err := h.predicate(ctx, binding, p.PR.Number)
	if err != nil {
		return nil, err
	}

	in := Input{
		Kind:                   EventKindReview,
		IssueStatus:            issue.Status,
		ReviewState:            ReviewState(strings.ToLower(p.Review.State)),
		ReviewByCR:             strings.EqualFold(p.Review.User.Login, binding.CrBotUsername),
		NoOpenCRChangesRequest: noOpenChanges,
		NoUnresolvedCRThreads:  noUnresolved,
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

	issue, found, err := h.resolveIssueByPR(ctx, binding.RepoFullName, p.PR.Number)
	if err != nil {
		return nil, err
	}
	if !found {
		return &dispatchResult{label: "issue_not_found"}, nil
	}

	noOpenChanges, noUnresolved, err := h.predicate(ctx, binding, p.PR.Number)
	if err != nil {
		return nil, err
	}
	in := Input{
		Kind:                   EventKindReviewThread,
		IssueStatus:            issue.Status,
		NoOpenCRChangesRequest: noOpenChanges,
		NoUnresolvedCRThreads:  noUnresolved,
	}
	dec := Decide(in)
	if dec.Action == ActionNoop {
		return &dispatchResult{label: "noop"}, nil
	}
	return h.applyDecision(ctx, issue, dec, p.PR, binding, "pull_request_review_thread")
}

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
		WorkspaceID:  binding.WorkspaceID,
		IssuePrefix:  id.Prefix,
		Number:       id.Number,
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

// ---------------------------------------------------------------------------
// CR predicate
// ---------------------------------------------------------------------------

func (h *WebhookHandler) predicate(ctx context.Context, binding db.WorkspaceRepoBinding, prNumber int32) (bool, bool, error) {
	owner, repo, ok := splitRepo(binding.RepoFullName)
	if !ok {
		return false, false, fmt.Errorf("invalid repo: %s", binding.RepoFullName)
	}
	var c PRReviewClient
	if h.NewClient != nil {
		c = h.NewClient(binding.InstallationID)
	} else {
		c = NewGitHubAPIClient(h.Auth, binding.InstallationID)
	}
	return EvaluatePredicate(ctx, c, owner, repo, int(prNumber), binding.CrBotUsername)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type prInfo struct {
	Number  int32  `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Merged  bool   `json:"merged"`
	State   string `json:"state"`
	Head    struct {
		Ref string `json:"ref"`
	} `json:"head"`
	HeadRef string `json:"-"`
}

type prPayload struct {
	Action string `json:"action"`
	PR     prInfo `json:"pull_request"`
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
	PR prInfo `json:"pull_request"`
}

type reviewThreadPayload struct {
	Action string `json:"action"`
	PR     prInfo `json:"pull_request"`
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

