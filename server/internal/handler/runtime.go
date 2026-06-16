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

func canEditRuntime(member db.Member, rt db.AgentRuntime) bool {
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
// cascade dialog without an extra round-trip. The cascade itself lives at
// POST /api/runtimes/:id/archive-agents-and-delete (ArchiveAgentsAndDeleteRuntime
// below) and runs the multi-write teardown inside a single transaction.
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
	// Surface them on the 409 so the dialog can render the cascade plan
	// directly from this response — saves a second round-trip when the
	// user clicked Delete from a stale list page.
	activeAgents, err := h.Queries.ListActiveAgentsByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime dependencies")
		return
	}
	if len(activeAgents) > 0 {
		writeJSON(w, http.StatusConflict, runtimeHasActiveAgentsResponse(activeAgents))
		return
	}

	// Refuse before any teardown-side effects if the runtime still has active
	// squads whose leader is already archived on this runtime.
	activeSquadCount, err := h.Queries.CountActiveSquadsWithArchivedLeadersByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime squad dependencies")
		return
	}
	if activeSquadCount > 0 {
		writeError(w, http.StatusConflict, "cannot delete runtime: it has active squads led by archived agents. Archive those squads or assign them a new leader first.")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Pause autopilots pointing at the archived agents BEFORE we delete
	// them. Migration 096 dropped the autopilot.assignee_id agent FK, so a
	// hard-delete here would otherwise leave dangling rows that subsequent
	// scheduler ticks would skip with "assignee agent no longer exists" —
	// quiet, but burning a run record every tick until an operator notices.
	// Pausing makes the breakage visible in the autopilot list so the owner
	// can re-point or delete the row instead. This runs inside the teardown
	// transaction so a pause that lands but is followed by a failed delete
	// rolls back with everything else, matching ArchiveAgentsAndDeleteRuntime.
	archivedAgentIDs, err := qtx.ListArchivedAgentIDsByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enumerate archived agents")
		return
	}
	if len(archivedAgentIDs) > 0 {
		if err := qtx.PauseAutopilotsByAgentAssignees(r.Context(), archivedAgentIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to pause autopilots")
			return
		}
	}

	// Remove archived squads whose leader is an archived agent on this runtime
	// so the RESTRICT FK on squad.leader_id won't block the subsequent agent
	// deletion. Active squads are handled by the 409 guard above instead.
	if err := qtx.DeleteSquadsByArchivedAgentsOnRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up squads referencing archived agents")
		return
	}

	// Remove archived agents so the FK constraint (ON DELETE RESTRICT) won't block deletion.
	if err := qtx.DeleteArchivedAgentsByRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up archived agents")
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

	slog.Info("runtime deleted", "runtime_id", uuidToString(rt.ID), "deleted_by", userID)

	// Notify frontend to refresh runtime list.
	h.publish(protocol.EventDaemonRegister, wsID, "member", userID, map[string]any{
		"action": "delete",
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// runtimeHasActiveAgentsResponse builds the structured 409 body shared by
// DeleteAgentRuntime (light-mode block) and ArchiveAgentsAndDeleteRuntime
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
func runtimeHasActiveAgentsResponse(agents []db.Agent) map[string]any {
	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		resp[i] = agentToResponse(a)
	}
	return map[string]any{
		"error":         "cannot delete runtime: it has active agents bound to it. Archive or reassign the agents first.",
		"code":          "runtime_has_active_agents",
		"active_agents": resp,
	}
}

// archiveAgentsAndDeleteRuntimeRequest is the wire shape for the cascade
// endpoint. expected_active_agent_ids is the snapshot the user just confirmed
// in the dialog — the server compares it to the live set inside the
// transaction and refuses with runtime_delete_plan_changed if anything moved
// between dialog open and confirm. That guarantees the user is approving the
// exact agent set that will be archived, even if a teammate adds or archives
// an agent in the same window.
type archiveAgentsAndDeleteRuntimeRequest struct {
	ExpectedActiveAgentIDs []string `json:"expected_active_agent_ids"`
}

// ArchiveAgentsAndDeleteRuntime is the cascade entry point: archive every
// agent currently bound to the runtime, cancel their queued/running tasks,
// pause autopilots that target them, hard-delete the now-detached archived
// rows so the agent.runtime_id FK no longer pins the runtime, and finally
// delete the runtime row itself — all inside a single transaction so a
// partial failure never leaves a runtime half-torn-down.
//
// Transaction order follows the reference revoke flow in
// revokeAndRemoveMember (workspace_revoke.go) so the two cascade paths share
// the same race-safety properties: the dispatcher can't claim a task whose
// runtime is about to vanish, autopilots can't fire onto a dead assignee,
// and post-commit publish events emit the same task:cancelled →
// agent:archived → daemon:register fan-out.
//
// The expected_active_agent_ids check is the load-bearing piece for the UX:
// the front-end snapshots the agent list when the dialog opens and presents
// the user a checkbox confirmation; if a teammate adds or archives an agent
// while that dialog is open, this endpoint refuses with
// runtime_delete_plan_changed and the latest list, so the user never confirms
// a stale plan.
func (h *Handler) ArchiveAgentsAndDeleteRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	var req archiveAgentsAndDeleteRuntimeRequest
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
	// new actives from appearing between our snapshot and our archive.
	if _, err := qtx.LockAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock runtime")
		return
	}

	// Re-list active agents inside the transaction, with FOR UPDATE on
	// each row so a concurrent archive/move of one of those existing
	// agents also blocks until we commit. Comparing against the expected
	// set here closes the dialog-open / user-confirm race: even if a
	// teammate creates or archives an agent on this runtime while the
	// dialog was open, the user is approving exactly the set the server
	// is about to archive.
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
		body := runtimeHasActiveAgentsResponse(currentActive)
		body["code"] = "runtime_delete_plan_changed"
		body["error"] = "the active agent set changed; please review and confirm again."
		writeJSON(w, http.StatusConflict, body)
		return
	}

	// Build the agent ID list once — it's the explicit allowlist for the
	// archive UPDATE below and the runtime-or-agent task cancel further
	// down. By keying the archive off this list (not off runtime_id) we
	// guarantee that agents not in the user's confirmed set can never
	// be silently archived, even if the row-level locks above somehow
	// missed something. Defense in depth.
	currentActiveIDs := make([]pgtype.UUID, len(currentActive))
	for i, a := range currentActive {
		currentActiveIDs[i] = a.ID
	}

	// 1. Archive every active agent on this runtime, narrowed to the
	//    user-confirmed expected_active_agent_ids set (which equals
	//    currentActive at this point). Returns the affected rows so the
	//    post-commit publish loop can fan out agent:archived per agent.
	archivedAgents, err := qtx.ArchiveAgentsByIDs(r.Context(), db.ArchiveAgentsByIDsParams{
		ArchivedBy: member.UserID,
		AgentIds:   currentActiveIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive agents")
		return
	}

	// 2. Cancel queued/dispatched/running tasks. Match by runtime_id AND
	//    by archived agent ids: agent.runtime_id can be reassigned without
	//    rewriting historical agent_task_queue rows, so an agent we just
	//    archived may still own tasks pinned to a different runtime — and
	//    ClaimAgentTask does not gate on agent.archived_at.
	archivedIDs := make([]pgtype.UUID, len(archivedAgents))
	for i, a := range archivedAgents {
		archivedIDs[i] = a.ID
	}
	cancelledTasks, err := qtx.CancelAgentTasksByRuntimeOrAgent(r.Context(), db.CancelAgentTasksByRuntimeOrAgentParams{
		RuntimeIds: []pgtype.UUID{rt.ID},
		AgentIds:   archivedIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel tasks")
		return
	}

	// 3. Pause autopilots whose assignee is one of the archived agents.
	//    Snapshots the full archived set on this runtime — including any
	//    that were already archived before this call — because the
	//    DeleteArchivedAgentsByRuntime below will hard-delete the lot, and
	//    a paused autopilot is much louder in the UI than a silently-
	//    dangling assignee_id (see migration 096 for why the FK is gone).
	allArchivedIDs, err := qtx.ListArchivedAgentIDsByRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enumerate archived agents")
		return
	}
	if len(allArchivedIDs) > 0 {
		if err := qtx.PauseAutopilotsByAgentAssignees(r.Context(), allArchivedIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to pause autopilots")
			return
		}
	}

	// 4. Hard-delete the archived agents so the agent.runtime_id FK
	//    (ON DELETE RESTRICT) no longer keeps the runtime alive.
	if err := qtx.DeleteArchivedAgentsByRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up archived agents")
		return
	}

	// 5. Finally delete the runtime row itself.
	if err := qtx.DeleteAgentRuntime(r.Context(), rt.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	// Post-commit fan-out — same ordering as publishRevocation so subscribers
	// observe task:cancelled before agent:archived before the runtime list
	// refresh, matching the order other revocation paths use.
	if h.TaskService != nil && len(cancelledTasks) > 0 {
		h.TaskService.BroadcastCancelledTasks(r.Context(), cancelledTasks)
	}
	for _, a := range archivedAgents {
		h.publish(protocol.EventAgentArchived, wsID, "member", userID, map[string]any{
			"agent": agentToResponse(a),
		})
	}
	h.publish(protocol.EventDaemonRegister, wsID, "member", userID, map[string]any{
		"action": "delete",
	})

	slog.Info("runtime deleted via cascade",
		"runtime_id", uuidToString(rt.ID),
		"deleted_by", userID,
		"agents_archived", len(archivedAgents),
		"tasks_cancelled", len(cancelledTasks),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"agents_archived": len(archivedAgents),
		"tasks_cancelled": len(cancelledTasks),
	})
}

// parseExpectedActiveAgentIDs validates the cascade endpoint's
// expected_active_agent_ids list. nil / empty is allowed (an empty set is a
// valid plan: "I confirmed there are no active agents" — the cascade then
// just deletes the runtime without archiving anything). Returns ok=false on
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
