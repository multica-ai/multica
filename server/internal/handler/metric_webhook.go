package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Workspace metric tokens (spec 4.1). An owner or admin mints a token for a
// workspace; an outside feed reports product readings with it to the webhook
// route. The token names its workspace, so a request can only write to that
// workspace, and every reading's scope is checked against it (ValidateScope).

// metricTokenReportsPerMinute bounds the readings one token may report a minute,
// across every metric it reports to. A metric's own limit still applies on top.
const metricTokenReportsPerMinute = 120

// metricTokenFallback limits token reports in-process when the shared limiter is
// not configured, as metricReportFallback does for member reports.
var metricTokenFallback = NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: metricTokenReportsPerMinute, Window: time.Minute})

// MetricTokenResponse is a token as the API lists it. The secret is not in it.
type MetricTokenResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  *string `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	RevokedAt  *string `json:"revoked_at"`
}

// CreateMetricTokenResponse is the token just minted: the one time its secret is
// returned.
type CreateMetricTokenResponse struct {
	Token string `json:"token"`
	MetricTokenResponse
}

// CreateMetricTokenRequest names a new token.
type CreateMetricTokenRequest struct {
	Name string `json:"name"`
}

func metricTokenResponse(t db.MetricWebhookToken) MetricTokenResponse {
	return MetricTokenResponse{
		ID: util.UUIDToString(t.ID), Name: t.Name,
		CreatedAt: timestampToNanoPtr(t.CreatedAt), LastUsedAt: timestampToNanoPtr(t.LastUsedAt),
		RevokedAt: timestampToNanoPtr(t.RevokedAt),
	}
}

// requireMetricTokenManager returns the caller's member row when it may manage
// metric tokens: an owner or an admin of the workspace.
func (h *Handler) requireMetricTokenManager(w http.ResponseWriter, r *http.Request) (db.Member, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Member{}, false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only an owner or admin manages metric tokens")
		return db.Member{}, false
	}
	return member, true
}

// ListMetricWebhookTokens lists the workspace's metric tokens, revoked ones
// included, newest first.
func (h *Handler) ListMetricWebhookTokens(w http.ResponseWriter, r *http.Request) {
	member, ok := h.requireMetricTokenManager(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListMetricWebhookTokens(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list metric tokens")
		return
	}
	out := make([]MetricTokenResponse, 0, len(rows))
	for _, t := range rows {
		out = append(out, metricTokenResponse(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

// CreateMetricWebhookToken mints a token. Only a person may do it (the route
// carries RequireHumanActor), so an agent cannot mint a feed credential.
func (h *Handler) CreateMetricWebhookToken(w http.ResponseWriter, r *http.Request) {
	member, ok := h.requireMetricTokenManager(w, r)
	if !ok {
		return
	}
	var req CreateMetricTokenRequest
	if !readCappedJSON(w, r, governanceJSONBodyLimit, &req, "invalid request body") {
		return
	}
	name := strings.TrimSpace(req.Name)
	if n := len([]rune(name)); n < 1 || n > 100 {
		writeError(w, http.StatusBadRequest, "name must be 1 to 100 characters")
		return
	}
	raw, err := auth.GenerateMetricWebhookToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint metric token")
		return
	}
	row, err := h.Queries.CreateMetricWebhookToken(r.Context(), db.CreateMetricWebhookTokenParams{
		ID: dbid.NewV7(), WorkspaceID: member.WorkspaceID, Name: name,
		TokenHash: auth.HashToken(raw), CreatedBy: member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save metric token")
		return
	}
	writeJSON(w, http.StatusCreated, CreateMetricTokenResponse{Token: raw, MetricTokenResponse: metricTokenResponse(row)})
}

// RevokeMetricWebhookToken stops a token from authenticating. The row stays as
// history. A token of another workspace is not found.
func (h *Handler) RevokeMetricWebhookToken(w http.ResponseWriter, r *http.Request) {
	member, ok := h.requireMetricTokenManager(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	row, err := h.Queries.RevokeMetricWebhookToken(r.Context(), db.RevokeMetricWebhookTokenParams{
		ID: id, WorkspaceID: member.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "metric token not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke metric token")
		return
	}
	writeJSON(w, http.StatusOK, metricTokenResponse(row))
}

// metricTokenFromRequest reads the token from the Authorization header (Bearer) or
// the X-Metric-Token header.
func metricTokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("X-Metric-Token"))
}

// allowMetricTokenReport spends one report from a token's budget.
func (h *Handler) allowMetricTokenReport(ctx context.Context, tokenID string) bool {
	bucket := "metric-token:" + tokenID
	if h.WebhookRateLimiter != nil {
		return h.WebhookRateLimiter.Allow(ctx, bucket)
	}
	return metricTokenFallback.Allow(ctx, bucket)
}

// ReportMetricReadingByToken records a reading from an outside feed. The token
// authenticates and names the workspace; the route takes no workspace header, so
// no request can write to a workspace its token does not belong to. The reading's
// scope is checked against that workspace by the metric service.
func (h *Handler) ReportMetricReadingByToken(w http.ResponseWriter, r *http.Request) {
	raw := metricTokenFromRequest(r)
	if !strings.HasPrefix(raw, "mwt_") {
		writeError(w, http.StatusUnauthorized, "a metric token is required")
		return
	}
	tok, err := h.Queries.GetActiveMetricWebhookTokenByHash(r.Context(), auth.HashToken(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "metric token is not valid")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check metric token")
		return
	}
	tokenID := util.UUIDToString(tok.ID)
	if !h.allowMetricTokenReport(r.Context(), tokenID) {
		writeError(w, http.StatusTooManyRequests, "too many readings from this token; try again in a minute")
		return
	}
	var req ReportMetricReadingRequest
	if !readCappedJSON(w, r, governanceJSONBodyLimit, &req, "invalid request body") {
		return
	}
	if req.Source != "" && req.Source != service.MetricSourceWebhook {
		writeError(w, http.StatusBadRequest, "a metric token reports webhook readings")
		return
	}
	_ = h.Queries.TouchMetricWebhookToken(r.Context(), tok.ID)
	h.recordMetricReading(w, r, tok.WorkspaceID, chi.URLParam(r, "key"), req,
		service.MetricSourceWebhook, service.ReadingActorWebhook, tok.ID)
}
