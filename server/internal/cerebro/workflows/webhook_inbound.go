package workflows

// Phase-3 inbound webhook ingress (JEH-1108, PR 2).
//
// Anonymous public endpoint at POST /api/cerebro/workflows/webhook/{token}.
// Mounted OUTSIDE the auth-required scope by router.go — the token IS the
// auth surface. Layered defenses:
//
//   * Token lookup — short-circuit on unknown / disabled rows with 404/403
//     so callers cannot probe for workflow existence.
//   * Timestamp window — required `X-Multica-Timestamp` (Unix seconds)
//     within ±5 minutes of now. Defeats trivial replay attacks even before
//     HMAC kicks in.
//   * Optional HMAC — when inbound_signing_secret IS NOT NULL on the row,
//     require `X-Multica-Signature` header containing
//     `sha256=<hex(hmac-sha256(secret, timestamp+"."+body))>`. Constant-time
//     compare. NULL secret is the "nem-mode" — token alone is legitimation.
//   * Body cap — 64 KB hard cap so a misbehaving caller can't exhaust memory.
//   * JSON decode — body must parse as a top-level object.
//   * issue_id — if the body has a top-level `issue_id` UUID that resolves
//     to a workspace-local issue, promote it onto te.IssueID; otherwise
//     leave empty (workspace-scoped event).
//
// 202 on accept; the action itself may be async (run_skill enqueues a
// task), so we don't wait for the action to complete.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// maxInboundWebhookBody — hard cap; 413 over this.
	maxInboundWebhookBody = 64 * 1024
	// inboundTimestampWindow — accepted clock skew between caller and us.
	inboundTimestampWindow = 5 * time.Minute
	// signaturePrefix — the prefix on the X-Multica-Signature header value.
	signaturePrefix = "sha256="
)

// IssueLookup is the narrow surface the webhook ingress needs to resolve
// an issue UUID from the request body and confirm it belongs to the
// workflow's workspace. Pulled into an interface so handler tests don't
// need a real DB.
//
// GetIssue mirrors db.Queries.GetIssue's signature. The handler stores a
// reference to db.Queries (the upstream queries handle) which satisfies
// this implicitly via the existing IssueActions interface, but webhook
// ingress only needs this one method so we keep the dependency minimal.
type IssueLookup interface {
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
}

// webhookInboundQueries is the narrow cerebrodb surface the ingress
// handler actually uses. Pulled into an interface so tests can fake the
// lookup without standing up a real *cerebrodb.Queries.
type webhookInboundQueries interface {
	GetCerebroWorkflowByInboundToken(ctx context.Context, token pgtype.Text) (cerebrodb.CerebroWorkflow, error)
}

// webhookInboundExecutor is the narrow Service surface the ingress handler
// uses. Tests stub this so the handler can be exercised without dragging
// the full retry-aware Service.Execute into the unit test.
type webhookInboundExecutor interface {
	Enabled() bool
	Execute(ctx context.Context, wf workflow, te TriggerEvent) error
	conditionsHold(ctx context.Context, wf workflow, te TriggerEvent) bool
}

// WebhookInboundHandler wires the public ingress endpoint. Constructed by
// router.go and mounted on the unauthenticated route group.
type WebhookInboundHandler struct {
	Cerebro    webhookInboundQueries
	Service    webhookInboundExecutor
	IssueQuery IssueLookup
}

// NewWebhookInboundHandler — Service is required for dispatch; IssueQuery
// is optional (nil → issue_id lookup is skipped entirely).
func NewWebhookInboundHandler(cerebro *cerebrodb.Queries, svc *Service, issues IssueLookup) *WebhookInboundHandler {
	return &WebhookInboundHandler{Cerebro: cerebro, Service: svc, IssueQuery: issues}
}

// ServeHTTP is exposed as a method directly so router.go can mount it
// without a wrapper closure.
func (h *WebhookInboundHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.handle(w, r)
}

func (h *WebhookInboundHandler) handle(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		writeWebhookError(w, http.StatusNotFound, "not found")
		return
	}
	tokenPrefix := token
	if len(tokenPrefix) > 8 {
		tokenPrefix = tokenPrefix[:8]
	}

	// 1. Lookup workflow by token. Unknown → 404; disabled → 403. Both
	// responses are intentionally bare so a probing caller can't tell which
	// state they hit beyond status code.
	row, err := h.Cerebro.GetCerebroWorkflowByInboundToken(r.Context(), pgtype.Text{String: token, Valid: true})
	if err != nil {
		if isNoRows(err) {
			slog.Info("workflow webhook inbound: unknown token",
				"token_prefix", tokenPrefix,
			)
			writeWebhookError(w, http.StatusNotFound, "not found")
			return
		}
		slog.Warn("workflow webhook inbound: lookup failed",
			"token_prefix", tokenPrefix,
			"error", err,
		)
		writeWebhookError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if row.TriggerType != TriggerWebhookInbound {
		// Token belongs to a workflow whose trigger isn't webhook_inbound —
		// shouldn't happen because tokens are only minted for webhook
		// workflows, but be defensive and refuse the request without
		// leaking which kind of workflow it actually is.
		slog.Info("workflow webhook inbound: token attached to wrong trigger",
			"token_prefix", tokenPrefix,
			"trigger_type", row.TriggerType,
		)
		writeWebhookError(w, http.StatusNotFound, "not found")
		return
	}
	if !row.Enabled {
		slog.Info("workflow webhook inbound: disabled",
			"token_prefix", tokenPrefix,
			"workflow_id", uuidString(row.ID),
		)
		writeWebhookError(w, http.StatusForbidden, "forbidden")
		return
	}

	// 2. Timestamp.
	tsHeader := r.Header.Get("X-Multica-Timestamp")
	if tsHeader == "" {
		slog.Info("workflow webhook inbound: missing timestamp",
			"token_prefix", tokenPrefix,
		)
		writeWebhookError(w, http.StatusUnauthorized, "missing timestamp")
		return
	}
	tsUnix, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		slog.Info("workflow webhook inbound: malformed timestamp",
			"token_prefix", tokenPrefix,
		)
		writeWebhookError(w, http.StatusUnauthorized, "malformed timestamp")
		return
	}
	ts := time.Unix(tsUnix, 0)
	now := nowFunc()
	delta := now.Sub(ts)
	if delta < 0 {
		delta = -delta
	}
	if delta > inboundTimestampWindow {
		slog.Info("workflow webhook inbound: timestamp outside window",
			"token_prefix", tokenPrefix,
			"delta_seconds", int(delta.Seconds()),
		)
		writeWebhookError(w, http.StatusUnauthorized, "timestamp outside allowed window")
		return
	}

	// 3. Body — read-limited so an oversized post returns 413 without
	// allocating a giant buffer. We read one extra byte so we can tell
	// "exactly at limit" from "over limit".
	body, tooBig, err := readLimitedBody(r.Body, maxInboundWebhookBody)
	if err != nil {
		slog.Warn("workflow webhook inbound: body read failed",
			"token_prefix", tokenPrefix,
			"error", err,
		)
		writeWebhookError(w, http.StatusBadRequest, "body read error")
		return
	}
	if tooBig {
		slog.Info("workflow webhook inbound: body too large",
			"token_prefix", tokenPrefix,
		)
		writeWebhookError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}

	// 4. Signature, only when the workflow has a secret set.
	signatureMode := "none"
	if row.InboundSigningSecret.Valid && row.InboundSigningSecret.String != "" {
		signatureMode = "checked"
		sig := r.Header.Get("X-Multica-Signature")
		if sig == "" {
			slog.Info("workflow webhook inbound: signature required but missing",
				"token_prefix", tokenPrefix,
			)
			writeWebhookError(w, http.StatusUnauthorized, "signature required")
			return
		}
		expected := computeInboundSignature(row.InboundSigningSecret.String, tsHeader, body)
		if !constantTimeEqualString(sig, expected) {
			slog.Info("workflow webhook inbound: signature mismatch",
				"token_prefix", tokenPrefix,
			)
			writeWebhookError(w, http.StatusUnauthorized, "signature mismatch")
			return
		}
	}

	slog.Info("workflow webhook inbound: accepted",
		"token_prefix", tokenPrefix,
		"workflow_id", uuidString(row.ID),
		"workspace_id", uuidString(row.WorkspaceID),
		"body_size", len(body),
		"signature_mode", signatureMode,
	)

	// 5. Decode body. Empty body is allowed (treated as empty object); any
	// other shape that isn't a JSON object is a 400.
	payload := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			writeWebhookError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	// 6. Optional issue_id resolution. Only set te.IssueID when the value
	// is (a) a valid UUID, (b) exists, and (c) belongs to this workflow's
	// workspace. Cross-workspace issue_id is silently dropped — the
	// workflow may still be useful even without an issue context.
	issueID := ""
	if h.IssueQuery != nil {
		if raw, ok := payload["issue_id"].(string); ok && raw != "" {
			if parsed, perr := util.ParseUUID(raw); perr == nil {
				issue, gerr := h.IssueQuery.GetIssue(r.Context(), parsed)
				if gerr == nil && uuidString(issue.WorkspaceID) == uuidString(row.WorkspaceID) {
					issueID = raw
				}
			}
		}
	}

	te := TriggerEvent{
		EventID:     buildInboundEventID(token, tsHeader, body),
		WorkspaceID: uuidString(row.WorkspaceID),
		IssueID:     issueID,
		Type:        TriggerWebhookInbound,
		Raw: map[string]any{
			"payload": payload,
		},
	}

	// Execute directly on this row — Dispatch would re-list every
	// webhook_inbound workflow in the workspace and fire them all on a
	// single token's request.
	wf := workflow{
		id:             row.ID,
		workspaceID:    row.WorkspaceID,
		projectID:      row.ProjectID,
		name:           row.Name,
		triggerType:    row.TriggerType,
		triggerConfig:  row.TriggerConfig,
		conditions:     row.Conditions,
		actionType:     row.ActionType,
		actionConfig:   row.ActionConfig,
		createdByID:    row.CreatedByID,
		createdByType:  row.CreatedByType,
		outboundSecret: row.OutboundWebhookSecret.String,
	}
	if !h.Service.conditionsHold(r.Context(), wf, te) {
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "skipped": "conditions"})
		return
	}
	if err := h.Service.Execute(r.Context(), wf, te); err != nil {
		slog.Error("workflow webhook inbound: execute failed",
			"token_prefix", tokenPrefix,
			"workflow_id", uuidString(row.ID),
			"error", err,
		)
		writeWebhookError(w, http.StatusInternalServerError, "execute failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// readLimitedBody reads up to max+1 bytes; if the +1th byte was reached the
// body was over the cap. Returns (truncatedBody, tooBig, err).
func readLimitedBody(body io.Reader, max int) ([]byte, bool, error) {
	if body == nil {
		return nil, false, nil
	}
	lim := io.LimitReader(body, int64(max)+1)
	buf, err := io.ReadAll(lim)
	if err != nil {
		return nil, false, err
	}
	if len(buf) > max {
		return nil, true, nil
	}
	return buf, false, nil
}

// computeInboundSignature builds the `sha256=<hex>` value we compare against
// the caller's X-Multica-Signature header. Format is timestamp + "." + body
// — the same pattern Stripe / Slack use.
func computeInboundSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// constantTimeEqualString — subtle.ConstantTimeCompare is byte-level; this
// is the string-friendly wrapper. Returns false for length-mismatch without
// further comparison (length disclosure is irrelevant for HMAC outputs since
// they're always the same length, but the early return keeps the call
// boring).
func constantTimeEqualString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// buildInboundEventID derives a deterministic event id from (token,
// timestamp, body-hash). Two identical retries collapse on the idempotency
// table; a fresh delivery (different body or different timestamp) produces
// a new id.
func buildInboundEventID(token, timestamp string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(token))
	h.Write([]byte("."))
	h.Write([]byte(timestamp))
	h.Write([]byte("."))
	h.Write(body)
	return "webhook_inbound:" + hex.EncodeToString(h.Sum(nil))
}

// writeWebhookError mirrors writeError but keeps the schema thin: callers
// never expose internal diagnostics through the webhook surface.
func writeWebhookError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

