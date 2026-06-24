package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type AgentRuntimeResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	DaemonID     *string `json:"daemon_id"`
	Name         string  `json:"name"`
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
	Visibility string  `json:"visibility"`
	LastSeenAt *string `json:"last_seen_at"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func runtimeToResponse(rt db.MulticaAgentRuntime) AgentRuntimeResponse {
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
		RuntimeMode:  rt.RuntimeMode,
		Provider:     rt.Provider,
		LaunchHeader: agent.LaunchHeader(rt.Provider),
		Status:       rt.Status,
		DeviceInfo:   rt.DeviceInfo,
		Metadata:     metadata,
		OwnerID:      uuidToPtr(rt.OwnerID),
		Visibility:   rt.Visibility,
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
			RuntimeID:        resolvedRuntimeID,
			Date:             row.Date.Time.Format("2006-01-02"),
			Provider:         row.Provider,
			Model:            row.Model,
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			CacheWriteTokens: row.CacheWriteTokens,
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

// RuntimeUsageByAgentResponse is one (agent, model) row of "Cost by agent".
// Model stays on the wire because cost is computed client-side from a model
// pricing table, intentionally not stored server-side so pricing changes
// don't require a back-fill. The client groups by agent_id and sums.
type RuntimeUsageByAgentResponse struct {
	AgentID          string `json:"agent_id"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	TaskCount        int32  `json:"task_count"`
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
			AgentID:          uuidToString(row.AgentID),
			Model:            row.Model,
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			CacheWriteTokens: row.CacheWriteTokens,
			TaskCount:        row.TaskCount,
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
	TaskCount        int32  `json:"task_count"`
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
			Hour:             int(row.Hour),
			Model:            row.Model,
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			CacheWriteTokens: row.CacheWriteTokens,
			TaskCount:        row.TaskCount,
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
	return pgtype.Timestamptz{Time: sinceFromDays(time.Now(), days, loc), Valid: true}
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
}

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
		// Notify connected clients that runtime metadata changed so the
		// list/detail pages refresh — matches the pattern used by
		// DeleteAgentRuntime.
		h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{
			"action": "update",
		})
	}

	writeJSON(w, http.StatusOK, runtimeToResponse(rt))
}

func canEditRuntime(member db.MulticaMember, rt db.MulticaAgentRuntime) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	return rt.OwnerID.Valid && uuidToString(rt.OwnerID) == uuidToString(member.UserID)
}

// canUseRuntimeForAgent reports whether a workspace member is allowed to
// bind a new agent to — or move an existing agent onto — the given runtime.
// Mirrors canEditRuntime but layers on the runtime's visibility flag so a
// `public` runtime is usable by anyone in the workspace while a `private`
// runtime stays bound to its owner. Workspace owners/admins keep an
// administrative override for both. See migration 083 for the visibility
// column.
func canUseRuntimeForAgent(member db.MulticaMember, rt db.MulticaAgentRuntime) bool {
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

	var runtimes []db.MulticaAgentRuntime
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

	// Check if any active (non-archived) agents are bound to this runtime.
	activeCount, err := h.Queries.CountActiveAgentsByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime dependencies")
		return
	}
	if activeCount > 0 {
		writeError(w, http.StatusConflict, "cannot delete runtime: it has active agents bound to it. Archive or reassign the agents first.")
		return
	}

	// Pause autopilots pointing at the archived agents BEFORE we delete
	// them. Migration 096 dropped the autopilot.assignee_id agent FK, so a
	// hard-delete here would otherwise leave dangling rows that subsequent
	// scheduler ticks would skip with "assignee agent no longer exists" —
	// quiet, but burning a run record every tick until an operator notices.
	// Pausing makes the breakage visible in the autopilot list so the owner
	// can re-point or delete the row instead.
	archivedAgentIDs, err := h.Queries.ListArchivedAgentIDsByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enumerate archived agents")
		return
	}
	if len(archivedAgentIDs) > 0 {
		if err := h.Queries.PauseAutopilotsByAgentAssignees(r.Context(), archivedAgentIDs); err != nil {
			slog.Warn("pause autopilots for archived agents failed",
				"runtime_id", uuidToString(rt.ID), "error", err)
		}
	}

	// Remove archived agents so the FK constraint (ON DELETE RESTRICT) won't block deletion.
	if err := h.Queries.DeleteArchivedAgentsByRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up archived agents")
		return
	}

	if err := h.Queries.DeleteAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}

	slog.Info("runtime deleted", "runtime_id", uuidToString(rt.ID), "deleted_by", userID)

	// Notify frontend to refresh runtime list.
	h.publish(protocol.EventDaemonRegister, wsID, "member", userID, map[string]any{
		"action": "delete",
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Runtime-level permissions (Design Two / L1.4) ─────────────────────────────

// RuntimePermissionRole is the effective role a user has on a runtime.
// "owner" is implicit via workspace owner/admin status or runtime.owner_id;
// explicit grants use "admin", "operator", or "viewer".
type RuntimePermissionRole string

const (
	RuntimeRoleOwner    RuntimePermissionRole = "owner"
	RuntimeRoleAdmin    RuntimePermissionRole = "admin"
	RuntimeRoleOperator RuntimePermissionRole = "operator"
	RuntimeRoleViewer   RuntimePermissionRole = "viewer"
)

// RuntimeCapabilities describes what a user may do with a runtime session.
type RuntimeCapabilities struct {
	Control bool `json:"control"` // takeover, handback, finalize, reject
	Observe bool `json:"observe"` // SSE/proxy session observation
}

// resolveRuntimeRole returns the effective runtime-level role for a member.
// Priority: workspace owner/admin > runtime owner > explicit grant.
func resolveRuntimeRole(member db.MulticaMember, rt db.MulticaAgentRuntime, explicitRole string) RuntimePermissionRole {
	if roleAllowed(member.Role, "owner", "admin") {
		return RuntimeRoleOwner
	}
	if rt.OwnerID.Valid && uuidToString(rt.OwnerID) == uuidToString(member.UserID) {
		return RuntimeRoleOwner
	}
	if explicitRole != "" {
		return RuntimePermissionRole(explicitRole)
	}
	return ""
}

// runtimeCapabilities maps a role to the actions it may perform.
func runtimeCapabilities(role RuntimePermissionRole) RuntimeCapabilities {
	switch role {
	case RuntimeRoleOwner, RuntimeRoleAdmin, RuntimeRoleOperator:
		return RuntimeCapabilities{Control: true, Observe: true}
	case RuntimeRoleViewer:
		return RuntimeCapabilities{Control: false, Observe: true}
	default:
		return RuntimeCapabilities{Control: false, Observe: false}
	}
}

// canControlRuntime reports whether the member can perform takeover/handback/finalize/reject.
func canControlRuntime(member db.MulticaMember, rt db.MulticaAgentRuntime, explicitRole string) bool {
	return runtimeCapabilities(resolveRuntimeRole(member, rt, explicitRole)).Control
}

// canObserveRuntime reports whether the member may observe SSE/proxy sessions on the runtime.
// Public runtimes are observable by any workspace member.
func canObserveRuntime(member db.MulticaMember, rt db.MulticaAgentRuntime, explicitRole string) bool {
	if rt.Visibility == "public" {
		return true
	}
	return runtimeCapabilities(resolveRuntimeRole(member, rt, explicitRole)).Observe
}

// requireRuntimePermission loads the runtime, verifies workspace membership,
// and resolves the effective runtime permission role. It writes 404/403 directly
// and returns ok=false on any failure.
func (h *Handler) requireRuntimePermission(w http.ResponseWriter, r *http.Request, runtimeID string) (member db.MulticaMember, rt db.MulticaAgentRuntime, role RuntimePermissionRole, ok bool) {
	runtimeUUID, parsed := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !parsed {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	member, ok = h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return
	}

	explicitRole := ""
	perm, err := h.Queries.GetRuntimePermission(r.Context(), db.GetRuntimePermissionParams{
		RuntimeID: rt.ID,
		UserID:    member.UserID,
	})
	if err == nil {
		explicitRole = perm.Role
	}

	role = resolveRuntimeRole(member, rt, explicitRole)
	return member, rt, role, true
}

// ── Runtime permission management ─────────────────────────────────────────────

type RuntimePermissionResponse struct {
	ID        string `json:"id"`
	RuntimeID string `json:"runtime_id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	UserName  string `json:"user_name,omitempty"`
	UserEmail string `json:"user_email,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type CreateRuntimePermissionRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type UpdateRuntimePermissionRequest struct {
	Role string `json:"role"`
}

func runtimePermissionToResponse(p db.MulticaRuntimePermission, userName, userEmail string) RuntimePermissionResponse {
	return RuntimePermissionResponse{
		ID:        uuidToString(p.ID),
		RuntimeID: uuidToString(p.RuntimeID),
		UserID:    uuidToString(p.UserID),
		Role:      p.Role,
		UserName:  userName,
		UserEmail: userEmail,
		CreatedAt: timestampToString(p.CreatedAt),
		UpdatedAt: timestampToString(p.UpdatedAt),
	}
}

// ListRuntimePermissions handles GET /api/runtimes/{runtimeId}/permissions.
// Any workspace member can list permissions for a runtime.
func (h *Handler) ListRuntimePermissions(w http.ResponseWriter, r *http.Request) {
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

	rows, err := h.Queries.ListRuntimePermissions(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list permissions")
		return
	}

	resp := make([]RuntimePermissionResponse, len(rows))
	for i, row := range rows {
		resp[i] = RuntimePermissionResponse{
			ID:        uuidToString(row.ID),
			RuntimeID: uuidToString(row.RuntimeID),
			UserID:    uuidToString(row.UserID),
			Role:      row.Role,
			UserName:  row.UserName,
			UserEmail: row.UserEmail,
			CreatedAt: timestampToString(row.CreatedAt),
			UpdatedAt: timestampToString(row.UpdatedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": resp})
}

// CreateRuntimePermission handles POST /api/runtimes/{runtimeId}/permissions.
// Only runtime owners and admins may grant permissions.
func (h *Handler) CreateRuntimePermission(w http.ResponseWriter, r *http.Request) {
	member, rt, role, ok := h.requireRuntimePermission(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	if role != RuntimeRoleOwner && role != RuntimeRoleAdmin {
		writeError(w, http.StatusForbidden, "only runtime owners and admins can grant permissions")
		return
	}

	var req CreateRuntimePermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.UserID == "" || req.Role == "" {
		writeError(w, http.StatusBadRequest, "user_id and role are required")
		return
	}
	if req.Role != string(RuntimeRoleAdmin) && req.Role != string(RuntimeRoleOperator) && req.Role != string(RuntimeRoleViewer) {
		writeError(w, http.StatusBadRequest, "role must be admin, operator, or viewer")
		return
	}

	userUUID, err := util.ParseUUID(req.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	if uuidToString(member.UserID) == req.UserID {
		writeError(w, http.StatusBadRequest, "cannot grant permission to yourself")
		return
	}

	perm, err := h.Queries.CreateRuntimePermission(r.Context(), db.CreateRuntimePermissionParams{
		RuntimeID: rt.ID,
		UserID:    userUUID,
		Role:      req.Role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "permission already exists for this user")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create permission")
		return
	}

	writeJSON(w, http.StatusCreated, runtimePermissionToResponse(perm, "", ""))
}

// UpdateRuntimePermission handles PATCH /api/runtimes/{runtimeId}/permissions/{userId}.
func (h *Handler) UpdateRuntimePermission(w http.ResponseWriter, r *http.Request) {
	_, rt, role, ok := h.requireRuntimePermission(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	if role != RuntimeRoleOwner && role != RuntimeRoleAdmin {
		writeError(w, http.StatusForbidden, "only runtime owners and admins can update permissions")
		return
	}

	userID := chi.URLParam(r, "userId")
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	var req UpdateRuntimePermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role != string(RuntimeRoleAdmin) && req.Role != string(RuntimeRoleOperator) && req.Role != string(RuntimeRoleViewer) {
		writeError(w, http.StatusBadRequest, "role must be admin, operator, or viewer")
		return
	}

	perm, err := h.Queries.UpdateRuntimePermissionRole(r.Context(), db.UpdateRuntimePermissionRoleParams{
		RuntimeID: rt.ID,
		UserID:    userUUID,
		Role:      req.Role,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "permission not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update permission")
		return
	}

	writeJSON(w, http.StatusOK, runtimePermissionToResponse(perm, "", ""))
}

// DeleteRuntimePermission handles DELETE /api/runtimes/{runtimeId}/permissions/{userId}.
func (h *Handler) DeleteRuntimePermission(w http.ResponseWriter, r *http.Request) {
	member, rt, role, ok := h.requireRuntimePermission(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	if role != RuntimeRoleOwner && role != RuntimeRoleAdmin {
		writeError(w, http.StatusForbidden, "only runtime owners and admins can revoke permissions")
		return
	}

	userID := chi.URLParam(r, "userId")
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	if uuidToString(member.UserID) == userID && role == RuntimeRoleOwner {
		writeError(w, http.StatusBadRequest, "runtime owner cannot revoke their own permission; transfer ownership first")
		return
	}

	if err := h.Queries.DeleteRuntimePermission(r.Context(), db.DeleteRuntimePermissionParams{
		RuntimeID: rt.ID,
		UserID:    userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete permission")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
