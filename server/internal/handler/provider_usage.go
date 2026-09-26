package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const providerUsageMaxBody = 64 * 1024

var providerUsageProviders = map[string]struct{}{
	"claude":      {},
	"cursor":      {},
	"codex":       {},
	"copilot":     {},
	"antigravity": {},
	"grok":        {},
	"kimi":        {},
	"kiro":        {},
	"opencode":    {},
}

var providerUsageReasons = map[string]struct{}{
	"not_logged_in":       {},
	"api_key_only":        {},
	"unauthorized":        {},
	"cli_unavailable":     {},
	"session_unavailable": {},
	"unsupported":         {},
}

var providerUsageForbiddenKeys = map[string]struct{}{
	"access_token":             {},
	"refresh_token":            {},
	"id_token":                 {},
	"token":                    {},
	"authorization":            {},
	"cookie":                   {},
	"cookies":                  {},
	"auth_json":                {},
	"password":                 {},
	"secret":                   {},
	"jwt":                      {},
	"session_token":            {},
	"bearer":                   {},
	"api_key":                  {},
	"openai_api_key":           {},
	"workoscursorsessiontoken": {},
}

// providerUsageReport is the daemon upload. Credential fields are rejected
// before this struct is trusted.
type providerUsageReport struct {
	Provider    string                      `json:"provider"`
	PlanName    string                      `json:"plan_name"`
	CollectedAt time.Time                   `json:"collected_at"`
	ReasonCode  string                      `json:"reason_code"`
	Windows     []providerUsageWindowReport `json:"windows"`
}

type providerUsageWindowReport struct {
	ID          string     `json:"id"`
	PercentUsed float64    `json:"percent_used"`
	ResetsAt    *time.Time `json:"resets_at"`
}

type providerUsageWindowResponse struct {
	ID          string  `json:"id"`
	PercentUsed float64 `json:"percent_used"`
	ResetsAt    *string `json:"resets_at,omitempty"`
}

type providerUsageSnapshotResponse struct {
	Provider    string                        `json:"provider"`
	PlanName    string                        `json:"plan_name,omitempty"`
	CollectedAt string                        `json:"collected_at"`
	ReasonCode  string                        `json:"reason_code,omitempty"`
	Windows     []providerUsageWindowResponse `json:"windows"`
}

type providerUsageResponse struct {
	Providers []providerUsageSnapshotResponse `json:"providers"`
}

type providerUsageBatchItem struct {
	RuntimeID string                          `json:"runtime_id"`
	Providers []providerUsageSnapshotResponse `json:"providers"`
}

type providerUsageBatchResponse struct {
	Runtimes []providerUsageBatchItem `json:"runtimes"`
}

// A machine hosts a handful of runtimes. The cap keeps the list read from
// turning into an unbounded IN list.
const providerUsageBatchMaxRuntimes = 64

// ReportProviderUsage stores one derived plan-limit snapshot for a runtime.
// The body is rejected when it carries a token, cookie, or auth.json field.
func (h *Handler) ReportProviderUsage(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, providerUsageMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := rejectCredentialFields(body); err != nil {
		writeError(w, http.StatusBadRequest, "request contains a credential field")
		return
	}
	var req providerUsageReport
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	normalized, err := normalizeProviderUsageReport(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store provider usage")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteRuntimeProviderUsage(r.Context(), db.DeleteRuntimeProviderUsageParams{
		RuntimeID:   rt.ID,
		WorkspaceID: rt.WorkspaceID,
		Provider:    normalized.Provider,
	}); err != nil {
		slog.Warn("delete provider usage failed", "runtime_id", uuidToString(rt.ID), "provider", normalized.Provider, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store provider usage")
		return
	}
	for _, row := range normalized.rows(rt.WorkspaceID, rt.ID) {
		if err := qtx.InsertRuntimeProviderUsage(r.Context(), row); err != nil {
			slog.Warn("insert provider usage failed", "runtime_id", uuidToString(rt.ID), "provider", normalized.Provider, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to store provider usage")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store provider usage")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetRuntimeProviderUsage returns the latest derived plan-limit snapshots for
// a runtime. It does not include task token accounting.
func (h *Handler) GetRuntimeProviderUsage(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	rows, err := h.Queries.ListRuntimeProviderUsage(r.Context(), db.ListRuntimeProviderUsageParams{
		WorkspaceID: rt.WorkspaceID,
		RuntimeID:   rt.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list provider usage")
		return
	}
	writeJSON(w, http.StatusOK, providerUsageResponseFromRows(rows))
}

// ListRuntimesProviderUsage returns derived plan-limit snapshots for the
// runtimes on one machine in a single read. Callers pass the runtime ids
// already on screen. Private runtimes the member cannot use are omitted.
func (h *Handler) ListRuntimesProviderUsage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	ids, ok := parseUUIDParamList(w, r.URL.Query().Get("runtime_ids"), "runtime_ids")
	if !ok {
		return
	}
	if len(ids) > providerUsageBatchMaxRuntimes {
		writeError(w, http.StatusBadRequest, "too many runtime_ids")
		return
	}
	empty := providerUsageBatchResponse{Runtimes: []providerUsageBatchItem{}}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	found, err := h.getAgentRuntimes(r.Context(), obsmetrics.RuntimeLookupSourceRuntimeAPI, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list provider usage")
		return
	}
	allowed := providerUsageReadableIDs(workspaceID, member, ids, found)
	if len(allowed) == 0 {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	rows, err := h.Queries.ListRuntimeProviderUsageByRuntimeIDs(r.Context(), db.ListRuntimeProviderUsageByRuntimeIDsParams{
		WorkspaceID: parseUUID(workspaceID),
		RuntimeIds:  allowed,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list provider usage")
		return
	}
	writeJSON(w, http.StatusOK, providerUsageBatchFromRows(allowed, rows))
}

type normalizedProviderUsage struct {
	Provider    string
	PlanName    string
	CollectedAt time.Time
	ReasonCode  string
	Windows     []providerUsageWindowReport
}

func (n normalizedProviderUsage) rows(workspaceID, runtimeID pgtype.UUID) []db.InsertRuntimeProviderUsageParams {
	plan := pgtype.Text{}
	if n.PlanName != "" {
		plan = pgtype.Text{String: n.PlanName, Valid: true}
	}
	collected := pgtype.Timestamptz{Time: n.CollectedAt, Valid: true}
	if len(n.Windows) == 0 {
		reason := pgtype.Text{String: n.ReasonCode, Valid: n.ReasonCode != ""}
		return []db.InsertRuntimeProviderUsageParams{{
			WorkspaceID: workspaceID,
			RuntimeID:   runtimeID,
			Provider:    n.Provider,
			WindowID:    "",
			PlanName:    plan,
			CollectedAt: collected,
			ReasonCode:  reason,
		}}
	}
	out := make([]db.InsertRuntimeProviderUsageParams, 0, len(n.Windows))
	for _, window := range n.Windows {
		resets := pgtype.Timestamptz{}
		if window.ResetsAt != nil {
			resets = pgtype.Timestamptz{Time: window.ResetsAt.UTC(), Valid: true}
		}
		out = append(out, db.InsertRuntimeProviderUsageParams{
			WorkspaceID: workspaceID,
			RuntimeID:   runtimeID,
			Provider:    n.Provider,
			WindowID:    window.ID,
			PercentUsed: pgtype.Float8{Float64: window.PercentUsed, Valid: true},
			ResetsAt:    resets,
			PlanName:    plan,
			CollectedAt: collected,
		})
	}
	return out
}

func normalizeProviderUsageReport(req providerUsageReport) (normalizedProviderUsage, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if _, ok := providerUsageProviders[provider]; !ok {
		return normalizedProviderUsage{}, errors.New("unknown provider")
	}
	if req.CollectedAt.IsZero() {
		return normalizedProviderUsage{}, errors.New("collected_at is required")
	}
	plan := strings.TrimSpace(req.PlanName)
	if len(plan) > 64 {
		return normalizedProviderUsage{}, errors.New("plan_name is too long")
	}
	if len(req.Windows) > 8 {
		return normalizedProviderUsage{}, errors.New("too many windows")
	}
	out := normalizedProviderUsage{
		Provider:    provider,
		PlanName:    plan,
		CollectedAt: req.CollectedAt.UTC(),
	}
	if len(req.Windows) == 0 {
		reason := strings.TrimSpace(req.ReasonCode)
		if reason == "" {
			reason = "not_logged_in"
		}
		if _, ok := providerUsageReasons[reason]; !ok {
			return normalizedProviderUsage{}, errors.New("unknown reason_code")
		}
		out.ReasonCode = reason
		return out, nil
	}
	out.Windows = make([]providerUsageWindowReport, 0, len(req.Windows))
	seen := map[string]struct{}{}
	for _, window := range req.Windows {
		id := strings.TrimSpace(window.ID)
		if !validWindowID(id) {
			return normalizedProviderUsage{}, errors.New("invalid window id")
		}
		if _, dup := seen[id]; dup {
			return normalizedProviderUsage{}, errors.New("duplicate window id")
		}
		seen[id] = struct{}{}
		if window.PercentUsed < 0 || window.PercentUsed > 1000 {
			return normalizedProviderUsage{}, errors.New("percent_used is out of range")
		}
		out.Windows = append(out.Windows, providerUsageWindowReport{
			ID:          id,
			PercentUsed: window.PercentUsed,
			ResetsAt:    window.ResetsAt,
		})
	}
	return out, nil
}

func validWindowID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if unicode.IsDigit(r) || (r >= 'a' && r <= 'z') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func rejectCredentialFields(body []byte) error {
	if len(bytesTrimSpace(body)) == 0 {
		return errors.New("empty")
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	if credentialValue(doc) {
		return errors.New("credential")
	}
	return nil
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func credentialValue(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for key, child := range t {
			if _, ok := providerUsageForbiddenKeys[strings.ToLower(key)]; ok {
				return true
			}
			if credentialValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if credentialValue(child) {
				return true
			}
		}
	case string:
		return credentialString(t)
	}
	return false
}

func credentialString(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if strings.Contains(lower, "workoscursorsessiontoken=") || strings.HasPrefix(lower, "bearer ") {
		return true
	}
	parts := strings.Split(s, ".")
	return len(parts) == 3 && len(s) >= 40 && strings.HasPrefix(parts[0], "eyJ")
}

type providerUsageWindowSource struct {
	Provider    string
	WindowID    string
	PercentUsed pgtype.Float8
	ResetsAt    pgtype.Timestamptz
	PlanName    pgtype.Text
	CollectedAt pgtype.Timestamptz
	ReasonCode  pgtype.Text
}

func providerUsageResponseFromRows(rows []db.ListRuntimeProviderUsageRow) providerUsageResponse {
	sources := make([]providerUsageWindowSource, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, providerUsageWindowSource{
			Provider:    row.Provider,
			WindowID:    row.WindowID,
			PercentUsed: row.PercentUsed,
			ResetsAt:    row.ResetsAt,
			PlanName:    row.PlanName,
			CollectedAt: row.CollectedAt,
			ReasonCode:  row.ReasonCode,
		})
	}
	return providerUsageResponse{Providers: snapshotsFromSources(sources)}
}

func providerUsageBatchFromRows(allowed []pgtype.UUID, rows []db.ListRuntimeProviderUsageByRuntimeIDsRow) providerUsageBatchResponse {
	grouped := map[string][]providerUsageWindowSource{}
	for _, row := range rows {
		key := uuidToString(row.RuntimeID)
		grouped[key] = append(grouped[key], providerUsageWindowSource{
			Provider:    row.Provider,
			WindowID:    row.WindowID,
			PercentUsed: row.PercentUsed,
			ResetsAt:    row.ResetsAt,
			PlanName:    row.PlanName,
			CollectedAt: row.CollectedAt,
			ReasonCode:  row.ReasonCode,
		})
	}
	resp := providerUsageBatchResponse{Runtimes: []providerUsageBatchItem{}}
	for _, id := range allowed {
		key := uuidToString(id)
		sources, ok := grouped[key]
		if !ok {
			continue
		}
		resp.Runtimes = append(resp.Runtimes, providerUsageBatchItem{
			RuntimeID: key,
			Providers: snapshotsFromSources(sources),
		})
	}
	return resp
}

// providerUsageReadableIDs keeps runtimes in this workspace that the member
// may use. Missing, cross-workspace, and private rows are dropped so the
// batch cannot confirm that a hidden runtime exists.
func providerUsageReadableIDs(workspaceID string, member db.Member, requested []pgtype.UUID, found map[string]db.AgentRuntime) []pgtype.UUID {
	seen := map[string]struct{}{}
	out := make([]pgtype.UUID, 0, len(requested))
	for _, id := range requested {
		key := uuidToString(id)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		rt, ok := found[key]
		if !ok || uuidToString(rt.WorkspaceID) != workspaceID {
			continue
		}
		if !canUseRuntimeForAgent(member, rt) {
			continue
		}
		out = append(out, rt.ID)
	}
	return out
}

func snapshotsFromSources(rows []providerUsageWindowSource) []providerUsageSnapshotResponse {
	order := make([]string, 0)
	byProvider := map[string]*providerUsageSnapshotResponse{}
	for _, row := range rows {
		snap, ok := byProvider[row.Provider]
		if !ok {
			collected := ""
			if row.CollectedAt.Valid {
				collected = row.CollectedAt.Time.UTC().Format(time.RFC3339)
			}
			snap = &providerUsageSnapshotResponse{
				Provider:    row.Provider,
				PlanName:    textValue(row.PlanName),
				CollectedAt: collected,
				Windows:     []providerUsageWindowResponse{},
			}
			byProvider[row.Provider] = snap
			order = append(order, row.Provider)
		}
		if row.WindowID == "" {
			snap.ReasonCode = textValue(row.ReasonCode)
			continue
		}
		window := providerUsageWindowResponse{ID: row.WindowID}
		if row.PercentUsed.Valid {
			window.PercentUsed = row.PercentUsed.Float64
		}
		if row.ResetsAt.Valid {
			formatted := row.ResetsAt.Time.UTC().Format(time.RFC3339)
			window.ResetsAt = &formatted
		}
		snap.Windows = append(snap.Windows, window)
	}
	out := make([]providerUsageSnapshotResponse, 0, len(order))
	for _, provider := range order {
		out = append(out, *byProvider[provider])
	}
	return out
}

func textValue(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}
