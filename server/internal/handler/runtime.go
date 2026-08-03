package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type AgentRuntimeResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	DaemonID    *string `json:"daemon_id"`
	Name        string  `json:"name"`
	// CustomName is the user-set display override (MUL-4217); null when the
	// runtime still uses its daemon-proposed Name. Clients show
	// CustomName ?? Name and seed the rename field from this raw value.
	CustomName   *string `json:"custom_name"`
	RuntimeMode  string  `json:"runtime_mode"`
	Provider     string  `json:"provider"`
	LaunchHeader string  `json:"launch_header"`
	Status       string  `json:"status"`
	DeviceInfo   string  `json:"device_info"`
	Metadata     any     `json:"metadata"`
	OwnerID      *string `json:"owner_id"`
	// Visibility is "private" (default — only the owner / workspace admins
	// can bind agents) or "public" (any workspace member can). See migration
	// 083 and canUseRuntimeForAgent.
	Visibility string `json:"visibility"`
	// ProfileID is set when this runtime is an instance of a custom
	// runtime_profile (MUL-3284); null for built-in runtimes.
	ProfileID  *string `json:"profile_id"`
	LastSeenAt *string `json:"last_seen_at"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func runtimeToResponse(rt db.AgentRuntime) AgentRuntimeResponse {
	var metadata any
	if rt.Metadata != nil {
		json.Unmarshal(rt.Metadata, &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}

	return AgentRuntimeResponse{
		ID:           uuidToString(rt.ID),
		WorkspaceID:  uuidToString(rt.WorkspaceID),
		DaemonID:     textToPtr(rt.DaemonID),
		Name:         rt.Name,
		CustomName:   textToPtr(rt.CustomName),
		RuntimeMode:  rt.RuntimeMode,
		Provider:     rt.Provider,
		LaunchHeader: agent.LaunchHeader(rt.Provider),
		Status:       rt.Status,
		DeviceInfo:   rt.DeviceInfo,
		Metadata:     metadata,
		OwnerID:      uuidToPtr(rt.OwnerID),
		Visibility:   rt.Visibility,
		ProfileID:    uuidToPtr(rt.ProfileID),
		LastSeenAt:   timestampToPtr(rt.LastSeenAt),
		CreatedAt:    timestampToString(rt.CreatedAt),
		UpdatedAt:    timestampToString(rt.UpdatedAt),
	}
}

// ---------------------------------------------------------------------------
// Runtime Usage
// ---------------------------------------------------------------------------

type RuntimeUsageResponse struct {
	RuntimeID        string `json:"runtime_id"`
	Date             string `json:"date"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	// Cost split: `CostUSDTicks` is what the provider itself charged for the
	// rows behind this aggregate (1e-10 USD), and the `Uncosted*` token
	// counts are the tokens from rows the provider did NOT price. The client
	// reports authoritative + estimate(uncosted), so a window mixing both
	// kinds of row stays whole. See migration 213.
	CostUSDTicks             int64 `json:"cost_usd_ticks"`
	UncostedInputTokens      int64 `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64 `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64 `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64 `json:"uncosted_cache_write_tokens"`
}

// GetRuntimeUsage returns daily token usage for a runtime, aggregated from
// per-task usage records captured by the daemon. This is scoped to
// Daemon-executed tasks only (i.e. excludes users' local CLI usage of the
// same tool).
func (h *Handler) GetRuntimeUsage(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	// All runtime reports render in the viewer's tz.
	viewTZ := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 90, viewTZ)

	resp, err := h.listRuntimeUsage(r.Context(), rt.ID, viewTZ, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// listRuntimeUsage reads the daily-bucketed trend from task_usage_hourly,
// applying the viewer's tz to project bucket_hour into local days.
func (h *Handler) listRuntimeUsage(ctx context.Context, runtimeID pgtype.UUID, tz string, since pgtype.Timestamptz) ([]RuntimeUsageResponse, error) {
	resolvedRuntimeID := uuidToString(runtimeID)
	rows, err := h.Queries.ListRuntimeUsage(ctx, db.ListRuntimeUsageParams{
		RuntimeID: runtimeID,
		Since:     since,
		Tz:        tz,
	})
	if err != nil {
		return nil, err
	}
	resp := make([]RuntimeUsageResponse, len(rows))
	for i, row := range rows {
		resp[i] = RuntimeUsageResponse{
			RuntimeID:                resolvedRuntimeID,
			Date:                     row.Date.Time.Format("2006-01-02"),
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
		}
	}
	return resp, nil
}

// GetRuntimeTaskActivity returns hourly task activity distribution for a runtime.
func (h *Handler) GetRuntimeTaskActivity(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	viewTZ := h.resolveViewingTZ(r)
	rows, err := h.Queries.GetRuntimeTaskHourlyActivity(r.Context(), db.GetRuntimeTaskHourlyActivityParams{
		RuntimeID: rt.ID,
		Tz:        viewTZ,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get task activity")
		return
	}

	type HourlyActivity struct {
		Hour  int `json:"hour"`
		Count int `json:"count"`
	}

	resp := make([]HourlyActivity, len(rows))
	for i, row := range rows {
		resp[i] = HourlyActivity{Hour: int(row.Hour), Count: int(row.Count)}
	}

	writeJSON(w, http.StatusOK, resp)
}

// RuntimeUsageByAgentResponse is one (agent, provider, model) row of "Cost by
// agent". provider + model stay on the wire because cost is computed
// client-side from a model pricing table (intentionally not stored server-side
// so pricing changes don't require a back-fill); provider disambiguates bare
// model ids that collide across providers. The client groups by agent_id and sums.
type RuntimeUsageByAgentResponse struct {
	AgentID          string `json:"agent_id"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	// Cost split: `CostUSDTicks` is what the provider itself charged for the
	// rows behind this aggregate (1e-10 USD), and the `Uncosted*` token
	// counts are the tokens from rows the provider did NOT price. The client
	// reports authoritative + estimate(uncosted), so a window mixing both
	// kinds of row stays whole. See migration 213.
	CostUSDTicks             int64 `json:"cost_usd_ticks"`
	UncostedInputTokens      int64 `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64 `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64 `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64 `json:"uncosted_cache_write_tokens"`
	TaskCount                int32 `json:"task_count"`
}

// GetRuntimeUsageByAgent returns per-agent token aggregates for a runtime
// since the cutoff window. Drives the runtime-detail "Cost by agent" tab.
func (h *Handler) GetRuntimeUsageByAgent(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	// No date bucketing — tz only sets the cutoff boundary so "last 30
	// days" means 30 of the viewer's days.
	viewTZ := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, viewTZ)

	rows, err := h.Queries.ListRuntimeUsageByAgent(r.Context(), db.ListRuntimeUsageByAgentParams{
		RuntimeID: rt.ID,
		Since:     since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by agent")
		return
	}

	resp := make([]RuntimeUsageByAgentResponse, len(rows))
	for i, row := range rows {
		resp[i] = RuntimeUsageByAgentResponse{
			AgentID:                  uuidToString(row.AgentID),
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
			TaskCount:                row.TaskCount,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// RuntimeUsageByHourResponse is one (hour, model) row. Hours with zero
// activity are omitted by the SQL — clients fill the gap to render a
// continuous 0..23 axis. Model is preserved for client-side cost math.
type RuntimeUsageByHourResponse struct {
	Hour             int    `json:"hour"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	// Cost split: `CostUSDTicks` is what the provider itself charged for the
	// rows behind this aggregate (1e-10 USD), and the `Uncosted*` token
	// counts are the tokens from rows the provider did NOT price. The client
	// reports authoritative + estimate(uncosted), so a window mixing both
	// kinds of row stays whole. See migration 213.
	CostUSDTicks             int64 `json:"cost_usd_ticks"`
	UncostedInputTokens      int64 `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64 `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64 `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64 `json:"uncosted_cache_write_tokens"`
	TaskCount                int32 `json:"task_count"`
}

// GetRuntimeUsageByHour returns hourly (0..23) token aggregates for a
// runtime since the cutoff window. Drives the "By hour" tab.
//
// The hour-of-day axis is bucketed in the viewer's tz like every other
// report — the same timezone resolved by resolveViewingTZ from the request's
// `?tz=` param or the authenticated user's stored user.timezone.
func (h *Handler) GetRuntimeUsageByHour(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	viewTZ := h.resolveViewingTZ(r)
	since := parseSinceParamInTZ(r, 30, viewTZ)

	rows, err := h.Queries.GetRuntimeUsageByHour(r.Context(), db.GetRuntimeUsageByHourParams{
		RuntimeID: rt.ID,
		Since:     since,
		Tz:        viewTZ,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get usage by hour")
		return
	}

	resp := make([]RuntimeUsageByHourResponse, len(rows))
	for i, row := range rows {
		resp[i] = RuntimeUsageByHourResponse{
			Hour:                     int(row.Hour),
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
			TaskCount:                row.TaskCount,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// sinceFromDays is the pure, now-injectable core of parseSinceParamInTZ.
// Given the current instant, a day count and an IANA location, it returns
// the instant of local midnight `days` days before `now`'s local calendar
// day. `now` is a parameter so the DST boundary maths can be tested at
// pinned dates (see TestSinceFromDays).
//
// The cutoff yields N+1 calendar buckets (today-days … today inclusive).
// The extra day versus a naive "-(days-1)" is deliberate headroom, not an
// off-by-one:
//   - Runtime detail's sliceWindow filters `date >= today-days` (closed) and
//     its prior-window delta reaches back to today-2*days, so the today-days
//     bucket MUST exist or the oldest bar / KPI delta silently loses data.
//   - The workspace dashboard re-filters client-side with -(days-1); the one
//     extra day the backend returns is trimmed there — harmless.
//
// Do not "tighten" this to -(days-1): it would break the runtime detail page.
func sinceFromDays(now time.Time, days int, loc *time.Location) time.Time {
	local := now.In(loc)
	startOfToday := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return startOfToday.AddDate(0, 0, -days)
}

// parseSinceParamInTZ parses the "days" query parameter into a cutoff
// timestamptz. Anchors the cutoff to start-of-day-(N) in the supplied IANA zone so that
// `days=N` returns full N+1 calendar buckets in that zone (today's partial
// bucket + N prior full days). If tzName is empty or unparseable, falls back
// to UTC — never returns an error so handlers stay simple.
func parseSinceParamInTZ(r *http.Request, defaultDays int, tzName string) pgtype.Timestamptz {
	return parseDaysCutoff(r, defaultDays, tzName, 0)
}

// parseExactSinceParamInTZ is parseSinceParamInTZ without the extra day of
// headroom: `days=N` yields exactly N calendar buckets (today's partial
// bucket + N-1 prior full days), which is the window the workspace dashboard
// actually displays.
//
// The N+1 cutoff exists so date-bucketed series can reach one bucket further
// back than they render (runtime detail's prior-window delta needs it), and
// the dashboard trims the surplus client-side with `-(days-1)`. A response
// with NO date dimension cannot be trimmed that way, so an aggregate served
// off the N+1 cutoff silently covers one more day than the chart beside it.
// Endpoints whose rows carry no date use this variant instead.
func parseExactSinceParamInTZ(r *http.Request, defaultDays int, tzName string) pgtype.Timestamptz {
	return parseDaysCutoff(r, defaultDays, tzName, 1)
}

// parseDaysCutoff is the shared body of the two cutoff parsers. `trimDays`
// pulls the cutoff forward, so 0 keeps the N+1 headroom and 1 closes the
// window to exactly N calendar days.
func parseDaysCutoff(
	r *http.Request,
	defaultDays int,
	tzName string,
	trimDays int,
) pgtype.Timestamptz {
	days := defaultDays
	if d := r.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	// Guard the floor: days is already >= 1 here, and trimming a 1-day
	// window by one would put the cutoff at start-of-today+0 — still correct
	// ("today only"), which is exactly what days=1 means.
	return pgtype.Timestamptz{
		Time:  sinceFromDays(time.Now(), days-trimDays, loc),
		Valid: true,
	}
}

// resolveViewingTZ resolves the IANA tz to render the response in:
// `?tz=` query param, else the authenticated user's stored
// user.timezone, else "UTC". Invalid values fall through rather than
// erroring — tz is a display concern.
//
// The browser app always sends `?tz=` (resolved client-side by
// useViewingTimezone), so the `GetUser` lookup below is a COLD fallback
// hit only by API clients / older builds that omit the param — it is not
// a hot path. Do not replicate this DB-read pattern into a handler that
// runs without a `?tz=`-supplying client in front of it.
func (h *Handler) resolveViewingTZ(r *http.Request) string {
	if tz := strings.TrimSpace(r.URL.Query().Get("tz")); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil && loc != nil {
			return tz
		}
	}
	if userID := requestUserID(r); userID != "" {
		uid, err := util.ParseUUID(userID)
		if err != nil {
			slog.Warn("resolveViewingTZ: malformed X-User-ID, falling back to UTC",
				"path", r.URL.Path, "user_id", userID)
		}
		if err == nil {
			slog.Debug("resolveViewingTZ cold path: ?tz= missing, reading user.timezone",
				"path", r.URL.Path, "user_id", userID)
			if user, err := h.Queries.GetUser(r.Context(), uid); err == nil && user.Timezone.Valid {
				stored := strings.TrimSpace(user.Timezone.String)
				if stored != "" {
					if loc, err := time.LoadLocation(stored); err == nil && loc != nil {
						return stored
					}
				}
			}
		}
	}
	return "UTC"
}

// UpdateAgentRuntimeRequest is the JSON body accepted by PATCH /api/runtimes/:id.
// Only fields users may legitimately edit are listed; other runtime metadata
// (provider, daemon_id, status…) flows in from the daemon and is read-only here.
type UpdateAgentRuntimeRequest struct {
	// Visibility flips a runtime between "private" (default — only the owner
	// or workspace admins can bind agents) and "public" (any workspace
	// member can). Owner / workspace admin only, gated by canEditRuntime.
	Visibility *string `json:"visibility,omitempty"`
	// CustomName sets or clears a user-facing display override (MUL-4217).
	// An empty / whitespace-only string clears it (revert to the
	// daemon-proposed name). Owner / workspace admin only.
	CustomName *string `json:"custom_name,omitempty"`
	// ApplyToMachine, when true alongside CustomName, applies the name to
	// every runtime sharing this runtime's daemon_id (a machine hosts one
	// runtime per provider) instead of just this one. Ignored when the
	// runtime has no daemon_id.
	ApplyToMachine bool `json:"apply_to_machine,omitempty"`
}

// maxRuntimeCustomNameLen caps a runtime's custom name. Default names are
// short (e.g. "Claude (host.local)"); 100 chars is generous headroom while
// keeping the picker rows and machine headers from overflowing.
const maxRuntimeCustomNameLen = 100

// UpdateAgentRuntime handles PATCH /api/runtimes/:id. Currently visibility
// is editable; the request shape is open-ended so future fields (display
// name, description) can be added without a route change.
// Workspace-membership-checked; write access is gated by canEditRuntime.
func (h *Handler) UpdateAgentRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "you can only edit your own runtimes")
		return
	}

	var req UpdateAgentRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Validate every field before any mutation so a bad value in one field
	// can't leave a partially-applied PATCH.
	var (
		newVisibility  string
		needVisibility bool
	)
	if req.Visibility != nil {
		v := *req.Visibility
		if v != "private" && v != "public" {
			writeError(w, http.StatusBadRequest, "visibility must be 'private' or 'public'")
			return
		}
		if v != rt.Visibility {
			newVisibility = v
			needVisibility = true
		}
	}

	if req.CustomName != nil {
		if len([]rune(strings.TrimSpace(*req.CustomName))) > maxRuntimeCustomNameLen {
			writeError(w, http.StatusBadRequest, "custom name is too long")
			return
		}
	}

	changed := false

	if needVisibility {
		updated, err := h.Queries.UpdateAgentRuntimeVisibility(r.Context(), db.UpdateAgentRuntimeVisibilityParams{
			ID:         runtimeUUID,
			Visibility: newVisibility,
		})
		if err != nil {
			slog.Error("UpdateAgentRuntimeVisibility failed", "error", err, "runtime_id", runtimeID)
			writeError(w, http.StatusInternalServerError, "failed to update runtime")
			return
		}
		rt = updated
		changed = true
	}

	if req.CustomName != nil {
		// An empty / whitespace-only name clears the override (NULL), so the
		// runtime falls back to its daemon-proposed Name.
		trimmed := strings.TrimSpace(*req.CustomName)
		customName := pgtype.Text{String: trimmed, Valid: trimmed != ""}

		if req.ApplyToMachine && rt.DaemonID.Valid {
			// Non-admins may only relabel their own runtimes on the machine;
			// owners/admins rename every runtime sharing the daemon_id. A NULL
			// owner filter means "all runtimes on this machine".
			var ownerFilter pgtype.UUID
			if !roleAllowed(member.Role, "owner", "admin") {
				ownerFilter = member.UserID
			}
			rows, err := h.Queries.UpdateAgentRuntimeCustomNameByDaemon(r.Context(), db.UpdateAgentRuntimeCustomNameByDaemonParams{
				CustomName:  customName,
				WorkspaceID: rt.WorkspaceID,
				DaemonID:    rt.DaemonID,
				OwnerID:     ownerFilter,
			})
			if err != nil {
				slog.Error("UpdateAgentRuntimeCustomNameByDaemon failed", "error", err, "runtime_id", runtimeID)
				writeError(w, http.StatusInternalServerError, "failed to update runtime")
				return
			}
			// The actor always owns (or admins) the runtime addressed by :id,
			// so it is among the updated rows — surface it in the response.
			for _, row := range rows {
				if uuidToString(row.ID) == uuidToString(runtimeUUID) {
					rt = row
					break
				}
			}
			changed = true
		} else {
			updated, err := h.Queries.UpdateAgentRuntimeCustomName(r.Context(), db.UpdateAgentRuntimeCustomNameParams{
				CustomName: customName,
				ID:         runtimeUUID,
			})
			if err != nil {
				slog.Error("UpdateAgentRuntimeCustomName failed", "error", err, "runtime_id", runtimeID)
				writeError(w, http.StatusInternalServerError, "failed to update runtime")
				return
			}
			rt = updated
			changed = true
		}
	}

	if changed {
		// Notify connected clients that runtime metadata changed so the
		// list/detail pages refresh — matches the pattern used by
		// DeleteAgentRuntime.
		h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{
			"action": "update",
		})
	}

	writeJSON(w, http.StatusOK, runtimeToResponse(rt))
}

func canEditRuntime(member db.Member, rt db.AgentRuntime) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	return rt.OwnerID.Valid && uuidToString(rt.OwnerID) == uuidToString(member.UserID)
}

func (h *Handler) runtimeHasLiveProfile(ctx context.Context, rt db.AgentRuntime) (bool, error) {
	if !rt.ProfileID.Valid {
		return false, nil
	}
	if _, err := h.Queries.GetRuntimeProfileForWorkspace(ctx, db.GetRuntimeProfileForWorkspaceParams{
		ID:          rt.ProfileID,
		WorkspaceID: rt.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// canUseRuntimeForAgent reports whether a workspace member is allowed to
// bind a new agent to — or move an existing agent onto — the given runtime.
// Mirrors canEditRuntime but layers on the runtime's visibility flag so a
// `public` runtime is usable by anyone in the workspace while a `private`
// runtime stays bound to its owner. Workspace owners/admins keep an
// administrative override for both. See migration 083 for the visibility
// column.
func canUseRuntimeForAgent(member db.Member, rt db.AgentRuntime) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	if rt.Visibility == "public" {
		return true
	}
	return rt.OwnerID.Valid && uuidToString(rt.OwnerID) == uuidToString(member.UserID)
}

func (h *Handler) ListAgentRuntimes(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	var runtimes []db.AgentRuntime
	var err error

	if ownerFilter := r.URL.Query().Get("owner"); ownerFilter == "me" {
		userID, ok := requireUserID(w, r)
		if !ok {
			return
		}
		runtimes, err = h.Queries.ListAgentRuntimesByOwner(r.Context(), db.ListAgentRuntimesByOwnerParams{
			WorkspaceID: parseUUID(workspaceID),
			OwnerID:     parseUUID(userID),
		})
	} else {
		runtimes, err = h.Queries.ListAgentRuntimes(r.Context(), parseUUID(workspaceID))
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runtimes")
		return
	}

	resp := make([]AgentRuntimeResponse, len(runtimes))
	for i, rt := range runtimes {
		resp[i] = runtimeToResponse(rt)
	}

	writeJSON(w, http.StatusOK, resp)
}

// DeleteAgentRuntime deletes a runtime after permission and dependency checks.
//
// The strict variant: refuses with 409 + structured `runtime_has_active_agents`
// when any non-archived agent is still bound to the runtime, and returns the
// blocking agent list in the response body so the front-end can pivot to the
// confirm dialog without an extra round-trip. The confirmed variant lives at
// POST /api/runtimes/:id/unbind-agents-and-delete (UnbindAgentsAndDeleteRuntime
// below) and runs the multi-write teardown inside a single transaction.
// errRuntimeNotDrained means the runtime still owned a non-terminal task after
// the teardown's own cancel pass. That should be impossible — it only happens if
// a new non-terminal task status was added without extending
// CancelAgentTasksByRuntimeOrAgent — so the teardown refuses rather than
// deleting the rows or tripping the agent_task_queue_active_requires_runtime
// CHECK with an opaque 500.
var errRuntimeNotDrained = errors.New("runtime still has non-terminal tasks")

// runtimeTeardownResult reports what the shared teardown changed so the caller
// can broadcast it after the transaction commits.
type runtimeTeardownResult struct {
	UnboundAgents    []db.Agent
	CancelledTasks   []db.AgentTaskQueue
	PausedAutopilots []db.Autopilot
}

// unbindRuntimeForDelete is the teardown every runtime-delete path runs inside
// its transaction, immediately before deleting the agent_runtime row (MUL-5559).
//
// It replaces the old archive-then-hard-delete of the runtime's agents. An agent
// is a persistent business object — identity, instructions, skills, chats,
// labels, channel installations, autopilot config — while a runtime is
// replaceable execution capacity, so retiring a machine unbinds its agents
// instead of destroying them. Unbound (runtime_id IS NULL) is a normal state,
// orthogonal to archived: service.AgentReadiness already refuses to give work to
// an agent with no runtime, and every trigger entry point reports
// agent_runtime_required.
//
// Order matters:
//
//  1. Unbind the user agents. Archived ones included: an agent archived earlier
//     is just as much the user's data. System agents are excluded — they are
//     invisible infrastructure with no rebind affordance, so they are deleted in
//     step 5 as before.
//  2. Pause active Autopilots assigned directly to those agents or to squads
//     they lead. The automation config stays intact and the persisted reason
//     explains that rebinding the Agent is the recovery path.
//  3. Cancel the non-terminal tasks of this runtime AND of the agents we just
//     unbound. The agent-side match is load-bearing: agent.runtime_id can move
//     without rewriting agent_task_queue.runtime_id, so a task an unbound agent
//     left pinned to another runtime would otherwise stay claimable while its
//     owner is no longer allowed to run.
//  4. Assert the runtime is drained (see errRuntimeNotDrained).
//  5. Detach the task history. Without this, deleting the runtime row would
//     cascade agent_task_queue away — and task_message / task_usage /
//     task_token with it — so the agents would survive with no record of what
//     they ever did.
//  6. Hard-delete the system agents, clearing first the rows whose cleanup has
//     no FK to follow (invocation targets, channel installations, chat pins,
//     labels, chat draft restores).
func unbindRuntimeForDelete(ctx context.Context, qtx *db.Queries, runtimeID pgtype.UUID) (runtimeTeardownResult, error) {
	var out runtimeTeardownResult

	unbound, err := qtx.UnbindUserAgentsFromRuntime(ctx, runtimeID)
	if err != nil {
		return out, fmt.Errorf("unbind agents: %w", err)
	}
	out.UnboundAgents = unbound

	unboundIDs := make([]pgtype.UUID, len(unbound))
	for i, a := range unbound {
		unboundIDs[i] = a.ID
	}
	paused, err := qtx.PauseAutopilotsByUnboundAgents(ctx, unboundIDs)
	if err != nil {
		return out, fmt.Errorf("pause autopilots: %w", err)
	}
	out.PausedAutopilots = paused

	cancelled, err := qtx.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{
		RuntimeIds: []pgtype.UUID{runtimeID},
		AgentIds:   unboundIDs,
	})
	if err != nil {
		return out, fmt.Errorf("cancel tasks: %w", err)
	}
	out.CancelledTasks = cancelled

	undrained, err := qtx.CountUndrainedTasksByRuntimeOrAgent(ctx, db.CountUndrainedTasksByRuntimeOrAgentParams{
		RuntimeIds: []pgtype.UUID{runtimeID},
		AgentIds:   unboundIDs,
	})
	if err != nil {
		return out, fmt.Errorf("count undrained tasks: %w", err)
	}
	if undrained > 0 {
		return out, fmt.Errorf("%w: %d", errRuntimeNotDrained, undrained)
	}
	if _, err := qtx.UnbindTasksFromRuntime(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("unbind task history: %w", err)
	}

	// agent_invocation_target has no agent_id FK (MUL-3963).
	if err := qtx.DeleteAgentInvocationTargetsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up agent invocation targets: %w", err)
	}
	// channel_* has no workspace/agent FK (MUL-3515 §4); an orphaned
	// installation would keep occupying its bot's (channel_type, app_id)
	// routing slot and make that bot un-rebindable (#4810).
	if err := qtx.DeleteChannelInstallationsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up channel installations: %w", err)
	}
	if err := qtx.DeleteChatPinnedAgentsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up chat pins: %w", err)
	}
	// agent_to_label has no agent_id FK.
	if err := qtx.DeleteAgentLabelAssignmentsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up agent label assignments: %w", err)
	}
	// chat_session cascades from agent and chat_draft_restore has no FK to
	// follow it (#5219), so prune the restores before the agent rows go.
	if err := pruneRuntimeSystemAgentChatDraftRestores(ctx, qtx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up chat draft restores: %w", err)
	}
	if err := qtx.DeleteSystemAgentsByRuntime(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up system agents: %w", err)
	}
	return out, nil
}

// publishRuntimeTeardown fans out a committed teardown. Ordering matches the
// other revocation paths: task:cancelled, then per-agent and Autopilot updates,
// then the runtime-list refresh.
func (h *Handler) publishRuntimeTeardown(ctx context.Context, res runtimeTeardownResult, wsID, userID string) {
	if h.TaskService != nil && len(res.CancelledTasks) > 0 {
		h.TaskService.BroadcastCancelledTasks(ctx, res.CancelledTasks)
	}
	for _, a := range res.UnboundAgents {
		// agent:status is the generic "this agent changed" broadcast the agent
		// update path already uses; subscribers refresh the row and see
		// runtime_bound=false. No agent:archived here — nothing was archived.
		h.publish(protocol.EventAgentStatus, wsID, "member", userID, map[string]any{
			"agent": broadcastAgentResponse(h.agentToResponse(a)),
		})
	}
	for _, a := range res.PausedAutopilots {
		h.publish(protocol.EventAutopilotUpdated, wsID, "member", userID, map[string]any{
			"autopilot": autopilotToResponse(a, nil),
		})
	}
	h.publish(protocol.EventDaemonRegister, wsID, "member", userID, map[string]any{
		"action": "delete",
	})
}

func (h *Handler) DeleteAgentRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found")
	if !ok {
		return
	}

	// Permission: owner/admin can delete any runtime; members can only delete their own.
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "you can only delete your own runtimes")
		return
	}
	userID := uuidToString(member.UserID)

	hasLiveProfile, err := h.runtimeHasLiveProfile(r.Context(), rt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime profile")
		return
	}
	if hasLiveProfile {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "cannot delete a custom runtime instance directly; delete its runtime profile instead.",
			"code":  "runtime_profile_instance_delete_unsupported",
		})
		return
	}
	if rt.ProfileID.Valid {
		slog.Warn("deleting orphaned profile-backed runtime instance",
			"runtime_id", uuidToString(rt.ID),
			"profile_id", uuidToString(rt.ProfileID),
			"workspace_id", wsID,
			"deleted_by", userID)
	}

	// Check if any active (non-archived) agents are bound to this runtime.
	// Surface them on the 409 so the dialog can render the cascade plan
	// directly from this response — saves a second round-trip when the
	// user clicked Delete from a stale list page.
	activeAgents, err := h.Queries.ListActiveAgentsByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime dependencies")
		return
	}
	// Refuse before any teardown-side effects while active agents are still
	// bound. The user confirms the plan through
	// POST /runtimes/:id/unbind-agents-and-delete, which reuses the same
	// teardown once the confirmed set is verified.
	if len(activeAgents) > 0 {
		writeJSON(w, http.StatusConflict, h.runtimeHasActiveAgentsResponse(activeAgents))
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Revalidate under the runtime row lock. Agent/task inserts take a
	// KEY SHARE lock through their runtime FK, so no active agent can appear
	// after this check and then be silently unbound by the teardown.
	if _, err := qtx.LockAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock runtime")
		return
	}
	if _, err := qtx.ListUserAgentsByRuntimeForUpdate(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock runtime dependencies")
		return
	}
	activeAgents, err = qtx.ListActiveAgentsByRuntimeForUpdate(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime dependencies")
		return
	}
	if len(activeAgents) > 0 {
		writeJSON(w, http.StatusConflict, h.runtimeHasActiveAgentsResponse(activeAgents))
		return
	}

	// Same teardown the confirmed path runs: unbind the runtime's agents and
	// their task history, cancel what was still active, remove only the system
	// agents. There is no active agent here by definition, but archived ones and
	// their history can still be bound to this runtime.
	teardown, err := unbindRuntimeForDelete(r.Context(), qtx, rt.ID)
	if err != nil {
		if errors.Is(err, errRuntimeNotDrained) {
			slog.Error("runtime delete aborted: tasks not drained",
				"runtime_id", uuidToString(rt.ID), "error", err)
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "the runtime still has tasks in flight; retry in a moment.",
				"code":  "runtime_delete_not_drained",
			})
			return
		}
		slog.Error("runtime delete teardown failed", "runtime_id", uuidToString(rt.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}

	if err := qtx.DeleteAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}

	slog.Info("runtime deleted",
		"runtime_id", uuidToString(rt.ID),
		"deleted_by", userID,
		"agents_unbound", len(teardown.UnboundAgents),
		"tasks_cancelled", len(teardown.CancelledTasks),
		"autopilots_paused", len(teardown.PausedAutopilots),
	)

	h.publishRuntimeTeardown(r.Context(), teardown, wsID, userID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// runtimeHasActiveAgentsResponse builds the structured 409 body shared by
// DeleteAgentRuntime (light-mode block) and UnbindAgentsAndDeleteRuntime
// (cascade-plan-changed). The shape is:
//
//	{
//	  "error": "...",
//	  "code":  "runtime_has_active_agents" | "runtime_delete_plan_changed",
//	  "active_agents": [AgentResponse, ...]
//	}
//
// Front-end branches on `code`. The caller picks which code to send; this
// helper just normalises the agent serialisation and the error string.
func (h *Handler) runtimeHasActiveAgentsResponse(agents []db.Agent) map[string]any {
	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		resp[i] = h.agentToResponse(a)
	}
	return map[string]any{
		"error":         "cannot delete runtime: it has active agents bound to it. Reassign them or confirm unbinding them first.",
		"code":          "runtime_has_active_agents",
		"active_agents": resp,
	}
}

// unbindAgentsAndDeleteRuntimeRequest is the wire shape for the confirmed
// delete endpoint. expected_active_agent_ids is the snapshot the user just
// confirmed in the dialog — the server compares it to the live set inside the
// transaction and refuses with runtime_delete_plan_changed if anything moved
// between dialog open and confirm. That guarantees the user is approving the
// exact agent set that will be unbound, even if a teammate adds or archives an
// agent in the same window.
//
// The compared set is deliberately still "active agents on this runtime": it is
// what installed clients send, and widening it would make every older client's
// request mismatch and 409 forever, leaving the runtime undeletable. Extra
// information for the dialog belongs in read-only fields, not in this set.
type unbindAgentsAndDeleteRuntimeRequest struct {
	ExpectedActiveAgentIDs []string `json:"expected_active_agent_ids"`
}

// UnbindAgentsAndDeleteRuntime is the confirmed delete entry point: unbind every
// user agent bound to the runtime, pause affected Autopilots, cancel active
// tasks, detach task history, hard-delete only the system agents, and finally
// delete the runtime row — all inside a single transaction so a partial failure
// never leaves a runtime half-torn-down.
//
// Before MUL-5559 this archived those agents and then hard-deleted the rows,
// destroying every conversation with them; the dialog said "archive", so what
// the user agreed to was not what happened. Now nothing of the user's is
// destroyed: the agents survive unbound and need a new runtime to run again.
//
// Transaction order follows the reference revoke flow in
// revokeAndRemoveMember (workspace_revoke.go) so the two paths share the same
// race-safety properties: the dispatcher can't claim a task whose runtime is
// about to vanish, and post-commit publish events emit the same
// task:cancelled → agent:status/autopilot:updated → daemon:register fan-out.
//
// The expected_active_agent_ids check is the load-bearing piece for the UX:
// the front-end snapshots the agent list when the dialog opens and presents
// the user a checkbox confirmation; if a teammate adds or archives an agent
// while that dialog is open, this endpoint refuses with
// runtime_delete_plan_changed and the latest list, so the user never confirms
// a stale plan.
//
// Served at POST /api/runtimes/:id/unbind-agents-and-delete and, for installed
// clients, the original /archive-agents-and-delete path.
func (h *Handler) UnbindAgentsAndDeleteRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	var req unbindAgentsAndDeleteRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	expected, ok := parseExpectedActiveAgentIDs(req.ExpectedActiveAgentIDs)
	if !ok {
		writeError(w, http.StatusBadRequest, "expected_active_agent_ids must be a list of valid UUIDs")
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found")
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "you can only delete your own runtimes")
		return
	}
	userID := uuidToString(member.UserID)

	hasLiveProfile, err := h.runtimeHasLiveProfile(r.Context(), rt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime profile")
		return
	}
	if hasLiveProfile {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "cannot delete a custom runtime instance directly; delete its runtime profile instead.",
			"code":  "runtime_profile_instance_delete_unsupported",
		})
		return
	}
	if rt.ProfileID.Valid {
		slog.Warn("deleting orphaned profile-backed runtime instance via cascade",
			"runtime_id", uuidToString(rt.ID),
			"profile_id", uuidToString(rt.ProfileID),
			"workspace_id", wsID,
			"deleted_by", userID)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Lock the runtime row first. PostgreSQL's FK validation on
	// agent.runtime_id requires FOR KEY SHARE on the parent runtime row,
	// which conflicts with FOR UPDATE — so any concurrent INSERT or
	// UPDATE that would point a new/moved agent at this runtime now
	// blocks until our tx finishes. This is the "兜底" lock that keeps
	// new actives from appearing between our snapshot and our unbind.
	if _, err := qtx.LockAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock runtime")
		return
	}
	if _, err := qtx.ListUserAgentsByRuntimeForUpdate(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock runtime dependencies")
		return
	}

	// Re-list active agents inside the transaction, with FOR UPDATE on
	// each row so a concurrent archive/move of one of those existing
	// agents also blocks until we commit. Comparing against the expected
	// set here closes the dialog-open / user-confirm race: even if a
	// teammate creates or archives an agent on this runtime while the
	// dialog was open, the user is approving exactly the set the server
	// is about to unbind.
	currentActive, err := qtx.ListActiveAgentsByRuntimeForUpdate(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enumerate active agents")
		return
	}
	if !activeAgentSetMatches(currentActive, expected) {
		// Refuse with the latest snapshot so the front-end can re-render
		// the dialog and force a fresh user confirmation. Reuses the
		// shared response helper but overrides the code to a planning
		// signal so the dialog can distinguish "you opened from a stale
		// page" from "the plan you confirmed just changed under you".
		body := h.runtimeHasActiveAgentsResponse(currentActive)
		body["code"] = "runtime_delete_plan_changed"
		body["error"] = "the active agent set changed; please review and confirm again."
		writeJSON(w, http.StatusConflict, body)
		return
	}

	// Single teardown, shared with the light DELETE path: unbind every user
	// agent (active and archived) plus their task history, cancel what was
	// running or queued, and hard-delete only the system agents. Nothing the
	// user configured is destroyed — the agents just need a new runtime.
	teardown, err := unbindRuntimeForDelete(r.Context(), qtx, rt.ID)
	if err != nil {
		if errors.Is(err, errRuntimeNotDrained) {
			slog.Error("runtime delete aborted: tasks not drained",
				"runtime_id", uuidToString(rt.ID), "error", err)
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "the runtime still has tasks in flight; retry in a moment.",
				"code":  "runtime_delete_not_drained",
			})
			return
		}
		slog.Error("runtime delete teardown failed", "runtime_id", uuidToString(rt.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unbind agents")
		return
	}

	// Finally delete the runtime row itself.
	if err := qtx.DeleteAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	h.publishRuntimeTeardown(r.Context(), teardown, wsID, userID)

	slog.Info("runtime deleted, agents unbound",
		"runtime_id", uuidToString(rt.ID),
		"deleted_by", userID,
		"agents_unbound", len(teardown.UnboundAgents),
		"tasks_cancelled", len(teardown.CancelledTasks),
		"autopilots_paused", len(teardown.PausedAutopilots),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ok",
		"agents_unbound":    len(teardown.UnboundAgents),
		"tasks_cancelled":   len(teardown.CancelledTasks),
		"autopilots_paused": len(teardown.PausedAutopilots),
		// Deprecated mirror of agents_unbound: installed clients built against
		// the archive-and-delete contract read this key. The count is the same
		// set of agents; they are no longer archived.
		"agents_archived": len(teardown.UnboundAgents),
	})
}

// parseExpectedActiveAgentIDs validates the cascade endpoint's
// expected_active_agent_ids list. nil / empty is allowed (an empty set is a
// valid plan: "I confirmed there are no active agents" — the cascade then
// just deletes the runtime without unbinding an active agent). Returns ok=false on
// any malformed UUID so the handler responds 400 instead of silently
// matching a different set.
func parseExpectedActiveAgentIDs(raw []string) (map[string]struct{}, bool) {
	out := make(map[string]struct{}, len(raw))
	for _, s := range raw {
		u, err := util.ParseUUID(s)
		if err != nil || !u.Valid {
			return nil, false
		}
		out[uuidToString(u)] = struct{}{}
	}
	return out, true
}

// activeAgentSetMatches reports whether the live set of active agents on the
// runtime matches the snapshot the front-end confirmed. Order-insensitive
// because the front-end may render in any order; size + membership is what
// matters for "did the plan change?".
func activeAgentSetMatches(current []db.Agent, expected map[string]struct{}) bool {
	if len(current) != len(expected) {
		return false
	}
	for _, a := range current {
		if _, ok := expected[uuidToString(a.ID)]; !ok {
			return false
		}
	}
	return true
}
