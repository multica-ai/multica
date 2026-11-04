package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The metric API (spec 4.1). Every member can read the metrics and their
// readings. A reading is reported by a member or an agent (a manual report), or
// by a member's token from outside the product (a webhook report, source
// "webhook"). Built-in metrics are computed and refuse reports.

// metricReportsPerMinute bounds the reports one workspace may make to one metric.
const metricReportsPerMinute = 60

// metricReportFallback limits reports in-process when the shared limiter is not
// configured (no Redis). It is per process: with several servers the shared
// limiter is the real bound.
var metricReportFallback = NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: metricReportsPerMinute, Window: time.Minute})

// MetricResponse is one metric.
type MetricResponse struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Unit        string `json:"unit"`
	Source      string `json:"source"`
	Direction   string `json:"direction"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

// MetricReadingResponse is one reading of a series.
type MetricReadingResponse struct {
	ID         string  `json:"id"`
	MetricKey  string  `json:"metric_key"`
	ScopeType  string  `json:"scope_type"`
	ScopeID    *string `json:"scope_id"`
	Value      float64 `json:"value"`
	ObservedAt string  `json:"observed_at"`
	ActorType  string  `json:"actor_type"`
	ActorID    *string `json:"actor_id"`
	Note       string  `json:"note"`
}

// ReportMetricReadingRequest is a reading to record. Name, unit and direction
// apply only when the report creates the metric. Source "webhook" marks a reading
// from an outside feed; the default is a manual report.
type ReportMetricReadingRequest struct {
	ScopeType  string   `json:"scope_type"`
	ScopeID    *string  `json:"scope_id"`
	Value      *float64 `json:"value"`
	ObservedAt *string  `json:"observed_at"`
	Note       string   `json:"note"`
	Source     string   `json:"source"`
	Name       string   `json:"name"`
	Unit       string   `json:"unit"`
	Direction  string   `json:"direction"`
}

func metricResponse(m db.Metric) MetricResponse {
	return MetricResponse{
		Key: m.Key, Name: m.Name, Unit: m.Unit, Source: m.Source, Direction: m.Direction,
		Description: m.Description, CreatedAt: timestampToString(m.CreatedAt),
	}
}

func metricReadingResponse(r db.MetricReading) MetricReadingResponse {
	value, _ := numericValue(r.Value)
	return MetricReadingResponse{
		ID: uuidToString(r.ID), MetricKey: r.MetricKey, ScopeType: r.ScopeType, ScopeID: uuidToPtr(r.ScopeID),
		Value: value, ObservedAt: timestampToString(r.ObservedAt), ActorType: r.ActorType,
		ActorID: uuidToPtr(r.ActorID), Note: r.Note,
	}
}

// numericValue reads a numeric column as a float64 for a response.
func numericValue(n pgtype.Numeric) (float64, bool) {
	if !n.Valid {
		return 0, false
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0, false
	}
	return f.Float64, true
}

// ListMetrics returns the workspace's metrics, built-in ones included.
func (h *Handler) ListMetrics(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	rows, err := service.NewMetricService(h.Queries, h.IssueService.TxStarter).ListMetrics(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("list metrics failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list metrics")
		return
	}
	out := make([]MetricResponse, 0, len(rows))
	for _, m := range rows {
		out = append(out, metricResponse(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": out})
}

// ListMetricReadings returns one series, newest first. scope_type defaults to
// company; scope_id is required for every other scope.
func (h *Handler) ListMetricReadings(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	scopeType, scopeID, ok := metricScopeFromQuery(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	p := service.MetricSeriesParams{
		WorkspaceID: wsUUID, Key: chi.URLParam(r, "key"), ScopeType: scopeType, ScopeID: scopeID,
	}
	if raw := q.Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since must be an RFC 3339 time")
			return
		}
		p.Since = t
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit must be from 1 to 500")
			return
		}
		p.Limit = int32(n)
	}
	rows, err := service.NewMetricService(h.Queries, h.IssueService.TxStarter).ListReadings(r.Context(), p)
	if err != nil {
		slog.Warn("list metric readings failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list readings")
		return
	}
	out := make([]MetricReadingResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, metricReadingResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"readings": out})
}

// metricScopeFromQuery reads scope_type and scope_id from a query string.
func metricScopeFromQuery(w http.ResponseWriter, r *http.Request) (string, pgtype.UUID, bool) {
	scopeType := r.URL.Query().Get("scope_type")
	if scopeType == "" {
		scopeType = service.ScopeCompany
	}
	var scopeID pgtype.UUID
	if raw := r.URL.Query().Get("scope_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "scope_id")
		if !ok {
			return "", pgtype.UUID{}, false
		}
		scopeID = id
	}
	return scopeType, scopeID, true
}

// ReportMetricReading records one reading. A member or an agent may report to a
// manual or webhook metric; the first report creates the metric. The scope is
// checked against the workspace, and the goals that read the metric are refreshed.
func (h *Handler) ReportMetricReading(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	c, ok := h.initiativeCallerFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	var req ReportMetricReadingRequest
	if !readCappedJSON(w, r, governanceJSONBodyLimit, &req, "invalid request body") {
		return
	}
	source, actorType, ok := metricSourceFor(w, req.Source, c.actor.Type)
	if !ok {
		return
	}
	h.recordMetricReading(w, r, wsUUID, key, req, source, actorType, c.actor.ID)
}

// metricSourceFor maps a report's source and the caller's kind to the stored
// source and actor type. A member or agent reports manual readings unless it
// names the webhook source.
func metricSourceFor(w http.ResponseWriter, reqSource, callerType string) (string, string, bool) {
	actorType := service.ReadingActorMember
	if callerType == "agent" {
		actorType = service.ReadingActorAgent
	}
	switch reqSource {
	case "", service.MetricSourceManual:
		return service.MetricSourceManual, actorType, true
	case service.MetricSourceWebhook:
		return service.MetricSourceWebhook, service.ReadingActorWebhook, true
	}
	writeError(w, http.StatusBadRequest, "source must be manual or webhook")
	return "", "", false
}

// recordMetricReading validates a reading, checks its scope against the workspace,
// rate-limits it per workspace and metric, writes it, and refreshes the goals that
// read the metric. The caller has already authenticated and chosen the source and
// actor.
func (h *Handler) recordMetricReading(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, key string, req ReportMetricReadingRequest, source, actorType string, actorID pgtype.UUID) {
	if req.Value == nil {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}
	scopeType := req.ScopeType
	if scopeType == "" {
		scopeType = service.ScopeCompany
	}
	var scopeID pgtype.UUID
	if req.ScopeID != nil && *req.ScopeID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, *req.ScopeID, "scope_id")
		if !ok {
			return
		}
		scopeID = parsed
	}
	var observed time.Time
	if req.ObservedAt != nil && *req.ObservedAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ObservedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "observed_at must be an RFC 3339 time")
			return
		}
		observed = t
	}
	if !h.allowMetricReport(r.Context(), util.UUIDToString(wsUUID), key) {
		writeError(w, http.StatusTooManyRequests, "too many readings for this metric; try again in a minute")
		return
	}

	svc := service.NewMetricService(h.Queries, h.IssueService.TxStarter)
	row, err := svc.RecordReading(r.Context(), service.ReadingInput{
		WorkspaceID: wsUUID, Key: key, Source: source, Name: req.Name, Unit: req.Unit, Direction: req.Direction,
		ScopeType: scopeType, ScopeID: scopeID, Value: *req.Value, ObservedAt: observed, Note: req.Note,
		ActorType: actorType, ActorID: actorID,
	}, time.Now())
	if err != nil {
		writeMetricError(w, err)
		return
	}
	// The goals reading this metric show the new reading at once.
	if _, err := h.goalService().RefreshConfidence(r.Context(), wsUUID); err != nil {
		slog.Warn("refresh goal confidence after reading failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"reading": metricReadingResponse(row)})
}

// allowMetricReport spends one report from the workspace's budget for a metric.
// The shared limiter is used when Redis is configured; otherwise an in-process
// one bounds the reports.
func (h *Handler) allowMetricReport(ctx context.Context, ws, key string) bool {
	bucket := "metric:" + ws + ":" + key
	if h.WebhookRateLimiter != nil {
		return h.WebhookRateLimiter.Allow(ctx, bucket)
	}
	return metricReportFallback.Allow(ctx, bucket)
}

func writeMetricError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrMetricBuiltinReadOnly),
		errors.Is(err, service.ErrMetricKeyInvalid),
		errors.Is(err, service.ErrMetricValueInvalid),
		errors.Is(err, service.ErrMetricScopeInvalid),
		errors.Is(err, service.ErrMetricObservedInvalid),
		errors.Is(err, service.ErrMetricNoteTooLong):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Warn("record metric reading failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record the reading")
	}
}

// RunMetricNightly is the work of the nightly metric job (see the scheduler).
func (h *Handler) RunMetricNightly(ctx context.Context) (int, error) {
	return h.goalService().RunMetricNightly(ctx, time.Now())
}
