package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

// maxWebhookBody bounds the payload we'll buffer. Real GitHub payloads
// max around 1 MB; we set 5 MB to be safe and reject anything larger
// before we burn memory verifying the signature on a malicious sender.
const maxWebhookBody = 5 * 1024 * 1024

// HandleGitHubWebhook is the public, unauthenticated webhook receiver.
//
// Flow:
//   1. Read body (capped) + envelope headers.
//   2. Locate the workspace whose secret verifies the HMAC. Reject 401
//      if no workspace matches; reject 400 on missing/malformed sig.
//   3. Insert into github_webhook_delivery for at-most-once dedup.
//      Duplicate -> respond 200 immediately (GitHub retried).
//   4. Spawn an async goroutine to dispatch the event into ship.Service.
//      The HTTP response returns 200 within milliseconds; processing
//      time stays off the request critical path.
//
// We accept that an async failure leaves the delivery row marked
// processed-with-error rather than retrying — GitHub's at-least-once
// delivery already covers transient failures. A hot retry loop here
// would risk a feedback storm under bad config.
func (h *Handler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	if len(body) > maxWebhookBody {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}

	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	eventType := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	signature := strings.TrimSpace(r.Header.Get("X-Hub-Signature-256"))
	if deliveryID == "" || eventType == "" {
		writeError(w, http.StatusBadRequest, "missing required headers")
		return
	}

	wsID, ok := h.verifyWebhookSignature(r.Context(), body, signature)
	if !ok {
		writeError(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	// Try to extract a repo URL from the body so the dedup row carries
	// a forensic hint even when the dispatch fails. Best-effort: a
	// missing field leaves it null, never aborts.
	repoURL := extractRepoURL(body)

	rec, err := h.Queries.RecordWebhookDelivery(r.Context(), db.RecordWebhookDeliveryParams{
		DeliveryID:  deliveryID,
		EventType:   eventType,
		WorkspaceID: wsID,
		RepoUrl:     pgtype.Text{String: repoURL, Valid: repoURL != ""},
	})
	if err != nil {
		slog.Warn("ship webhook: record delivery failed", "delivery_id", deliveryID, "error", err)
		// Defensive: still 200 so GitHub doesn't retry-storm us. The
		// missing dedup means we *might* re-process if this delivery
		// later succeeds-then-retries, but the upserts in ship.Service
		// are idempotent so the cost is bounded.
		writeJSON(w, http.StatusOK, map[string]any{"received": true, "deduped": false})
		return
	}
	if !rec.Inserted {
		// Duplicate: GitHub re-delivered. Already processed (or in
		// flight) — return 200 fast.
		writeJSON(w, http.StatusOK, map[string]any{"received": true, "deduped": true})
		return
	}

	// Hand off to the async worker. We capture the body + envelope
	// because r.Context is cancelled when the handler returns.
	go h.dispatchWebhook(WebhookDispatch{
		DeliveryID:  deliveryID,
		EventType:   eventType,
		WorkspaceID: wsID,
		Body:        body,
	})

	writeJSON(w, http.StatusOK, map[string]any{"received": true, "deduped": false})
}

// WebhookDispatch is the async-handoff envelope. Exported so a future
// background-queue implementation can serialize it; for now the
// goroutine path is good enough for a single-node API.
type WebhookDispatch struct {
	DeliveryID  string
	EventType   string
	WorkspaceID pgtype.UUID
	Body        []byte
}

// dispatchWebhook is the async worker. It runs ship.Service.ProcessWebhook
// against a fresh background context (the original request is gone) and
// publishes a WS event for the outcome.
func (h *Handler) dispatchWebhook(d WebhookDispatch) {
	// Use a request-scoped timeout so a misbehaving handler can't pin
	// the goroutine. 30s is generous; most paths finish in milliseconds.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	defer func() {
		// chi.Recoverer doesn't see goroutines we launched after the
		// handler returned. Recover defensively so a panic in
		// ProcessWebhook never tears the server down.
		if r := recover(); r != nil {
			slog.Error("ship webhook: panic in dispatch",
				"delivery_id", d.DeliveryID, "panic", r)
			_ = h.Queries.MarkWebhookDeliveryProcessed(context.Background(), db.MarkWebhookDeliveryProcessedParams{
				DeliveryID: d.DeliveryID,
				Error:      pgtype.Text{String: fmt.Sprintf("panic: %v", r), Valid: true},
			})
		}
	}()

	ws, err := h.Queries.GetWorkspace(ctx, d.WorkspaceID)
	if err != nil {
		h.markDeliveryError(d.DeliveryID, fmt.Errorf("get workspace: %w", err))
		return
	}
	token := readShipHubGitHubToken(ws.Settings)
	// Token may also live in the encrypted store. The webhook path
	// rarely calls back to GitHub; a missing token only matters for the
	// `push` event that triggers a SyncProject.
	if token == "" {
		if encToken, ok := h.readEncryptedToken(ctx, d.WorkspaceID); ok {
			token = encToken
		}
	}

	svc := &ship.Service{
		Q:      h.Queries,
		Github: gh.NewClient(token),
	}
	outcome, err := svc.ProcessWebhook(ctx, ship.WebhookEvent{
		WorkspaceID: d.WorkspaceID,
		DeliveryID:  d.DeliveryID,
		EventType:   d.EventType,
		Body:        d.Body,
	})
	if err != nil {
		h.markDeliveryError(d.DeliveryID, err)
		slog.Warn("ship webhook: dispatch failed",
			"delivery_id", d.DeliveryID, "event", d.EventType, "error", err)
		return
	}
	if err := h.Queries.MarkWebhookDeliveryProcessed(context.Background(), db.MarkWebhookDeliveryProcessedParams{
		DeliveryID: d.DeliveryID,
		Error:      pgtype.Text{},
	}); err != nil {
		slog.Warn("ship webhook: mark processed failed",
			"delivery_id", d.DeliveryID, "error", err)
	}

	// Phase 4 — channel auto-create on PR open / first detection,
	// auto-archive on PR close+merge. Both run after the outcome is
	// computed so the channel link is published in the next render
	// (the WS event arrives via Mark*Processed → frontend re-fetches).
	h.maybeManagePRConversationChannel(ctx, ws, outcome)

	h.publishWebhookOutcome(d.WorkspaceID, outcome)
}

// maybeManagePRConversationChannel inspects the webhook outcome and
// either creates or archives the PR's conversation channel. Best-effort:
// every failure path logs and returns rather than propagating.
func (h *Handler) maybeManagePRConversationChannel(ctx context.Context, ws db.Workspace, o ship.WebhookOutcome) {
	if o.Kind != "pr_state_changed" || !o.PRID.Valid {
		return
	}
	pr := o.PR
	switch o.PRAction {
	case "opened", "reopened":
		// Re-read the PR to make sure linkage updates have flushed.
		latest, err := h.Queries.GetPullRequest(ctx, pr.ID)
		if err == nil {
			pr = latest
		}
		if pr.ConversationChannelID.Valid {
			return
		}
		if _, err := h.createPRConversationChannel(ctx, ws.ID, ws, pr); err != nil {
			slog.Warn("ship webhook: auto-create conversation channel failed",
				"pr_id", uuidToString(pr.ID), "error", err)
		}
	case "closed":
		// On close (merged or not), archive + snapshot. The processPullRequest
		// already handled the auto-close-issue branch.
		h.archivePRConversationChannel(ctx, ws.ID, pr)
	}
}

// publishWebhookOutcome translates a service outcome into the right WS
// event. Centralized so the dispatcher doesn't have to know event
// constants.
func (h *Handler) publishWebhookOutcome(wsID pgtype.UUID, o ship.WebhookOutcome) {
	wsIDStr := uuidToString(wsID)
	switch o.Kind {
	case "pr_state_changed":
		h.publish(protocol.EventPullRequestStateChanged, wsIDStr, "system", "", map[string]any{
			"project_id":      uuidToString(o.ProjectID),
			"pr_id":           uuidToString(o.PRID),
			"state":           o.State,
			"ci_status":       o.CIStatus,
			"review_decision": o.ReviewDec,
		})
	case "deploy_progress":
		h.publish(protocol.EventDeployProgress, wsIDStr, "system", "", map[string]any{
			"environment_id": uuidToString(o.EnvironmentID),
			"deploy_id":      uuidToString(o.DeployID),
			"status":         o.DeployStatus,
			"sha":            o.SHA,
		})
	case "deploy_completed":
		h.publish(protocol.EventDeployCompleted, wsIDStr, "system", "", map[string]any{
			"environment_id": uuidToString(o.EnvironmentID),
			"sha":            o.SHA,
			"status":         o.DeployStatus,
		})
	}
}

// markDeliveryError records the error against the delivery row without
// rolling back dedup. Same atomicity the rest of webhook processing
// relies on: at-most-once delivery still applies.
func (h *Handler) markDeliveryError(deliveryID string, err error) {
	_ = h.Queries.MarkWebhookDeliveryProcessed(context.Background(), db.MarkWebhookDeliveryProcessedParams{
		DeliveryID: deliveryID,
		Error:      pgtype.Text{String: err.Error(), Valid: true},
	})
}

// verifyWebhookSignature scans every workspace with a configured secret
// and returns the workspace whose HMAC matches. We deliberately try
// every secret rather than letting the client tell us which workspace
// they belong to — an unauthenticated header can't be trusted.
//
// Performance: at the scale Ship Hub is designed for (small teams), the
// total number of workspaces with webhook secrets is tiny. If this
// ever becomes a bottleneck, a cached secret-by-id map keyed off
// workspace.updated_at solves it without changing the public API.
func (h *Handler) verifyWebhookSignature(ctx context.Context, body []byte, signature string) (pgtype.UUID, bool) {
	if signature == "" {
		return pgtype.UUID{}, false
	}
	encRows, err := h.Queries.ListWorkspacesWithEncryptedWebhookSecret(ctx)
	if err == nil {
		for _, row := range encRows {
			plaintext, decErr := secrets.DecryptString(row.ValueEncrypted)
			if decErr != nil {
				slog.Warn("ship webhook: decrypt secret failed", "workspace_id", row.WorkspaceID, "error", decErr)
				continue
			}
			if gh.VerifySignature(body, signature, plaintext) == nil {
				return row.WorkspaceID, true
			}
		}
	}
	plainRows, err := h.Queries.ListWorkspacesWithWebhookSecret(ctx)
	if err != nil {
		slog.Warn("ship webhook: list plaintext secrets failed", "error", err)
		return pgtype.UUID{}, false
	}
	for _, row := range plainRows {
		if !row.ShipHubWebhookSecret.Valid || row.ShipHubWebhookSecret.String == "" {
			continue
		}
		if gh.VerifySignature(body, signature, row.ShipHubWebhookSecret.String) == nil {
			return row.ID, true
		}
	}
	return pgtype.UUID{}, false
}

// readEncryptedToken returns the workspace's GitHub PAT from the
// encrypted-at-rest store. Returns ok=false when not present (the
// fallback path in callers tries the legacy settings JSON next).
func (h *Handler) readEncryptedToken(ctx context.Context, wsID pgtype.UUID) (string, bool) {
	row, err := h.Queries.GetWorkspaceSecret(ctx, db.GetWorkspaceSecretParams{
		WorkspaceID: wsID,
		Name:        "github_token",
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("ship webhook: load encrypted token", "workspace_id", wsID, "error", err)
		}
		return "", false
	}
	plaintext, err := secrets.DecryptString(row.ValueEncrypted)
	if err != nil {
		slog.Warn("ship webhook: decrypt token failed", "workspace_id", wsID, "error", err)
		return "", false
	}
	return plaintext, true
}

// extractRepoURL grabs repository.html_url from any payload for the
// audit log. Returns "" when the field is absent or the JSON can't be
// parsed — never errors out.
func extractRepoURL(body []byte) string {
	var probe struct {
		Repository struct {
			HTMLURL string `json:"html_url"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return probe.Repository.HTMLURL
}

// RegenerateShipHubWebhookSecret is the workspace-owner-only endpoint
// that mints a fresh secret, persists it (encrypted when a key is
// configured), and returns the plaintext exactly once.
//
// Mirrors the personal-access-token create flow: clients MUST capture
// the response value because the server will never re-display it.
// Subsequent GET /workspaces/{id} only reports `webhook_secret_set`.
func (h *Handler) RegenerateShipHubWebhookSecret(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return
	}
	plaintext, err := secrets.GenerateRandomURLSafe()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}

	// Try to store encrypted. If LoadKey fails (misconfigured), fall
	// back to the plaintext column so dev environments aren't bricked
	// by a missing env var.
	encrypted, encErr := secrets.EncryptString(plaintext)
	if encErr == nil {
		if _, err := h.Queries.UpsertWorkspaceSecret(r.Context(), db.UpsertWorkspaceSecretParams{
			WorkspaceID:    wsID,
			Name:           "github_webhook_secret",
			ValueEncrypted: encrypted,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist secret")
			return
		}
		// Drop any stale plaintext from the workspace column so the
		// signature verifier doesn't see two valid candidates.
		_ = h.Queries.SetWorkspaceWebhookSecretPlaintext(r.Context(), db.SetWorkspaceWebhookSecretPlaintextParams{
			ID:                   wsID,
			ShipHubWebhookSecret: pgtype.Text{},
		})
	} else {
		slog.Warn("ship webhook: encryption failed, falling back to plaintext", "error", encErr)
		if err := h.Queries.SetWorkspaceWebhookSecretPlaintext(r.Context(), db.SetWorkspaceWebhookSecretPlaintextParams{
			ID:                   wsID,
			ShipHubWebhookSecret: pgtype.Text{String: plaintext, Valid: true},
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist secret")
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"webhook_secret":     plaintext,
		"webhook_url":        webhookPublicURL(),
		"webhook_secret_set": true,
	})
}

// webhookPublicURL is the public address clients should configure on
// GitHub. Read from MULTICA_API_BASE_URL with a sensible default for
// local dev.
func webhookPublicURL() string {
	base := strings.TrimRight(os.Getenv("MULTICA_API_BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:8080"
	}
	return base + "/api/integrations/github/webhook"
}
