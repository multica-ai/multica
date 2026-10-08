package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Custom Runtime Profiles (MUL-3284)
//
// A runtime_profile is a workspace-level, team-shared definition of a custom
// runtime — e.g. an in-house Codex wrapper. Daemons pull the enabled profiles
// for their workspace, resolve command_name on PATH, and register an
// agent_runtime instance carrying the profile_id. The profile only changes how
// a runtime is launched/displayed. runtime_type selects a supported compatibility
// target, and its descriptor determines the underlying protocol_family.
//
// Iron rule: a profile carries NO generic per-agent args. Per-agent launch args
// stay on agent.custom_args. The only args field is fixed_args — args every
// agent on this runtime must inherit to enter a compatible mode.
// ---------------------------------------------------------------------------

type RuntimeProfileResponse struct {
	ID             string   `json:"id"`
	WorkspaceID    string   `json:"workspace_id"`
	DisplayName    string   `json:"display_name"`
	ProtocolFamily string   `json:"protocol_family"`
	RuntimeType    string   `json:"runtime_type"`
	CommandName    string   `json:"command_name"`
	Description    *string  `json:"description"`
	FixedArgs      []string `json:"fixed_args"`
	Visibility     string   `json:"visibility"`
	CreatedBy      *string  `json:"created_by"`
	Enabled        bool     `json:"enabled"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

func runtimeProfileToResponse(p db.RuntimeProfile) RuntimeProfileResponse {
	args := []string{}
	if len(p.FixedArgs) > 0 {
		_ = json.Unmarshal(p.FixedArgs, &args)
		if args == nil {
			args = []string{}
		}
	}
	return RuntimeProfileResponse{
		ID:             uuidToString(p.ID),
		WorkspaceID:    uuidToString(p.WorkspaceID),
		DisplayName:    p.DisplayName,
		ProtocolFamily: p.ProtocolFamily,
		RuntimeType:    agent.ProfileRuntimeType(p.RuntimeType, p.ProtocolFamily),
		CommandName:    p.CommandName,
		Description:    textToPtr(p.Description),
		FixedArgs:      args,
		Visibility:     p.Visibility,
		CreatedBy:      uuidToPtr(p.CreatedBy),
		Enabled:        p.Enabled,
		CreatedAt:      timestampToString(p.CreatedAt),
		UpdatedAt:      timestampToString(p.UpdatedAt),
	}
}

// NOTE: runtime_profile.visibility is intentionally NOT user-settable in v1.
// The column exists and the API still returns it, but creation always forces
// 'workspace': the daemon-pull, DaemonRegister and ListRuntimeProfiles read
// paths do not yet enforce 'private', so accepting 'private' from a client
// would silently leak a "private" profile's name/command to other members and
// let other machines' daemons register it (lateral data leak). Re-expose a
// visibility control only once those read paths enforce creator visibility.
// Follow-up: MUL-3308.
const runtimeProfileDefaultVisibility = "workspace"

// marshalFixedArgs validates and JSON-encodes the fixed_args list. Each entry
// must be a non-empty string; the column defaults to an empty array.
func marshalFixedArgs(args []string) ([]byte, error) {
	if len(args) == 0 {
		return []byte("[]"), nil
	}
	clean := make([]string, 0, len(args))
	for _, a := range args {
		// fixed_args are launch flags inherited by every agent on the runtime;
		// blank entries are always a client mistake.
		if strings.TrimSpace(a) == "" {
			return nil, errors.New("fixed_args entries must be non-empty")
		}
		if strings.ContainsRune(a, '\x00') {
			return nil, errors.New("fixed_args entries cannot contain NUL bytes")
		}
		clean = append(clean, a)
	}
	return json.Marshal(clean)
}

func validateRuntimeProfileCommandName(commandName string) error {
	if commandName == "" {
		return errors.New("command_name is required")
	}
	if strings.ContainsAny(commandName, " \t\r\n") {
		return errors.New("command_name must be a single executable token; put arguments in fixed_args")
	}
	if strings.ContainsRune(commandName, '\x00') {
		return errors.New("command_name cannot contain NUL bytes")
	}
	return nil
}

type createRuntimeProfileRequest struct {
	DisplayName    string   `json:"display_name"`
	ProtocolFamily string   `json:"protocol_family"`
	RuntimeType    string   `json:"runtime_type"`
	CommandName    string   `json:"command_name"`
	Description    *string  `json:"description"`
	FixedArgs      []string `json:"fixed_args"`
	Enabled        *bool    `json:"enabled"`
}

// CreateRuntimeProfile creates a workspace runtime profile. Admin-gated by the
// router. protocol_family is validated against the agent backend whitelist.
func (h *Handler) CreateRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	member, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}

	var req createRuntimeProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.ProtocolFamily = strings.TrimSpace(req.ProtocolFamily)
	req.CommandName = strings.TrimSpace(req.CommandName)

	if req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}
	req.RuntimeType = agent.ProfileRuntimeType(strings.TrimSpace(req.RuntimeType), req.ProtocolFamily)
	family, supported := agent.RuntimeProtocolFamily(req.RuntimeType)
	if !supported {
		writeError(w, http.StatusBadRequest, "unsupported runtime_type: "+req.RuntimeType)
		return
	}
	if req.ProtocolFamily != "" && req.ProtocolFamily != family {
		writeError(w, http.StatusBadRequest, "protocol_family does not match runtime_type")
		return
	}
	req.ProtocolFamily = family
	if req.CommandName == "" {
		writeError(w, http.StatusBadRequest, "command_name is required")
		return
	}
	if err := validateRuntimeProfileCommandName(req.CommandName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fixedArgs, err := marshalFixedArgs(req.FixedArgs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	profile, err := h.Queries.CreateRuntimeProfile(r.Context(), db.CreateRuntimeProfileParams{
		WorkspaceID:    wsUUID,
		DisplayName:    req.DisplayName,
		ProtocolFamily: req.ProtocolFamily,
		RuntimeType:    req.RuntimeType,
		CommandName:    req.CommandName,
		Description:    ptrToText(req.Description),
		FixedArgs:      fixedArgs,
		Visibility:     runtimeProfileDefaultVisibility,
		CreatedBy:      member.UserID,
		Enabled:        enabled,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a runtime profile with this display_name already exists")
			return
		}
		slog.Error("CreateRuntimeProfile failed", "error", err, "workspace_id", wsID)
		writeError(w, http.StatusInternalServerError, "failed to create runtime profile")
		return
	}

	profileID := uuidToString(profile.ID)
	h.requestDaemonRuntimeProfileRefresh(wsID, profileID)
	h.publish(protocol.EventDaemonRegister, wsID, "member", uuidToString(member.UserID), map[string]any{
		"runtime_profile_id": profileID,
	})

	writeJSON(w, http.StatusCreated, runtimeProfileToResponse(profile))
}

// ListRuntimeProfiles returns every runtime profile in the workspace.
// Member-gated by the router.
func (h *Handler) ListRuntimeProfiles(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	if _, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}

	profiles, err := h.Queries.ListRuntimeProfiles(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runtime profiles")
		return
	}
	resp := make([]RuntimeProfileResponse, len(profiles))
	for i, p := range profiles {
		resp[i] = runtimeProfileToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtime_profiles": resp})
}

// GetRuntimeProfile returns one runtime profile. Member-gated by the router.
func (h *Handler) GetRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	if _, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	profileUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "profileId"), "profile id")
	if !ok {
		return
	}

	profile, err := h.Queries.GetRuntimeProfileForWorkspace(r.Context(), db.GetRuntimeProfileForWorkspaceParams{
		ID:          profileUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime profile not found")
		return
	}
	writeJSON(w, http.StatusOK, runtimeProfileToResponse(profile))
}

type updateRuntimeProfileRequest struct {
	RuntimeType    *string   `json:"runtime_type"`
	ProtocolFamily *string   `json:"protocol_family"`
	DisplayName    *string   `json:"display_name"`
	CommandName    *string   `json:"command_name"`
	Description    *string   `json:"description"`
	FixedArgs      *[]string `json:"fixed_args"`
	Enabled        *bool     `json:"enabled"`
}

// UpdateRuntimeProfile applies a partial update. runtime_type and protocol_family are immutable
// (changing it would silently repoint bound agents onto a different backend).
// Admin-gated by the router.
func (h *Handler) UpdateRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	member, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	profileUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "profileId"), "profile id")
	if !ok {
		return
	}

	var req updateRuntimeProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RuntimeType != nil || req.ProtocolFamily != nil {
		writeError(w, http.StatusBadRequest, "runtime_type and protocol_family are immutable; create a new profile")
		return
	}

	params := db.UpdateRuntimeProfileParams{ID: profileUUID, WorkspaceID: wsUUID}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if name == "" {
			writeError(w, http.StatusBadRequest, "display_name cannot be empty")
			return
		}
		params.DisplayName = strToText(name)
	}
	if req.CommandName != nil {
		cmd := strings.TrimSpace(*req.CommandName)
		if err := validateRuntimeProfileCommandName(cmd); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.CommandName = strToText(cmd)
	}
	if req.Description != nil {
		params.Description = ptrToText(req.Description)
	}
	if req.FixedArgs != nil {
		fixedArgs, err := marshalFixedArgs(*req.FixedArgs)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.FixedArgs = fixedArgs
	}
	if req.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}

	profile, err := h.Queries.UpdateRuntimeProfile(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "runtime profile not found")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a runtime profile with this display_name already exists")
			return
		}
		slog.Error("UpdateRuntimeProfile failed", "error", err, "profile_id", uuidToString(profileUUID))
		writeError(w, http.StatusInternalServerError, "failed to update runtime profile")
		return
	}

	profileID := uuidToString(profile.ID)
	h.requestDaemonRuntimeProfileRefresh(wsID, profileID)
	h.publish(protocol.EventDaemonRegister, wsID, "member", uuidToString(member.UserID), map[string]any{
		"runtime_profile_id": profileID,
	})

	writeJSON(w, http.StatusOK, runtimeProfileToResponse(profile))
}

// maxNamedBlockingAgents caps how many agents the refusal spells out before it
// falls back to a count. Enough to recognise the machine they sit on without
// turning a CLI error into a wall of text.
const maxNamedBlockingAgents = 5

// maxReportedBlockingAgents caps both the rows read inside the delete
// transaction and the entries put on the response. A profile accumulates agents
// across every machine that registered it, and neither the sentence nor any
// client needs the whole set to do its job — the exact size travels separately
// as active_agent_count.
const maxReportedBlockingAgents = 20

// profileDeleteBlockedByAgents explains which agents are keeping this profile
// alive and, crucially, which machine each one is on.
//
// A profile is workspace-wide, so its bound agents are frequently on a
// different machine than the stale instance the user is actually trying to
// clean up. The old message said only "active agents are still bound to its
// runtimes", which left that user with no way to tell whether the blocker was
// the dead machine or the healthy one — and the natural next move, unbinding
// agents that were working fine, is exactly the damage worth preventing
// (GH #8456).
// agents is the bounded sample the query returned; its first row carries the
// full-set totals every caller here needs.
//
// The recovery paths come from those totals, never from the sample. Which rows
// survive the LIMIT is arbitrary with respect to class — twenty ordinary agents
// sorting first push the one Mika to position 21 — so advice derived from the
// visible rows would tell the user to archive blockers that cannot be archived,
// which is the defect this whole change set removes.
func profileDeleteBlockedByAgents(profileName string, agents []db.ListActiveAgentsByProfileRow) map[string]any {
	if len(agents) == 0 {
		// Caller only builds a refusal when there is at least one blocker.
		return map[string]any{
			"error": "cannot delete this custom runtime profile: active agents are still bound to its runtimes.",
			"code":  "runtime_profile_has_active_agents",
		}
	}
	summary := agents[0]
	total := summary.TotalCount

	classes := map[blockingAgentClass]bool{
		blockingAgentUser:           summary.UserCount > 0,
		blockingAgentMika:           summary.MikaCount > 0,
		blockingAgentBuilderCarrier: summary.AgentBuilderCount > 0,
		blockingAgentOtherSystem:    summary.OtherSystemCount > 0,
	}

	named := make([]string, 0, maxNamedBlockingAgents)
	for _, a := range agents {
		class := blockingAgentClassFromKey(a.BlockerClass)
		if len(named) >= maxNamedBlockingAgents {
			continue
		}
		runtimeName := a.RuntimeName
		if a.RuntimeCustomName.Valid && strings.TrimSpace(a.RuntimeCustomName.String) != "" {
			runtimeName = a.RuntimeCustomName.String
		}
		named = append(named, blockingAgentLabel(a.Name, runtimeName, a.RuntimeStatus, class))
	}
	listed := strings.Join(named, ", ")
	if remaining := total - int64(len(named)); remaining > 0 {
		listed = fmt.Sprintf("%s, and %d more", listed, remaining)
	}

	subject := "this custom runtime profile"
	if strings.TrimSpace(profileName) != "" {
		subject = fmt.Sprintf("the custom runtime profile %q", profileName)
	}

	remedies := blockingAgentRemedies(classes, blockingAgentScopeProfile)
	if len(remedies) == 0 {
		remedies = []string{"None of them can be released from here."}
	}

	resp := make([]map[string]any, len(agents))
	for i, a := range agents {
		resp[i] = map[string]any{
			"id":           uuidToString(a.ID),
			"name":         a.Name,
			"kind":         a.Kind,
			"system_key":   textToPtr(a.SystemKey),
			"runtime_id":   uuidToString(a.RuntimeID),
			"runtime_name": a.RuntimeName,
			// Deliberately separate from runtime_name: a client that renders
			// its own copy shows custom_name ?? name, same as everywhere else.
			"runtime_custom_name": textToPtr(a.RuntimeCustomName),
			"runtime_status":      a.RuntimeStatus,
		}
	}

	sentences := append([]string{fmt.Sprintf(
		"cannot delete %s: %d active agent(s) are still bound to its runtimes — %s.",
		subject, total, listed,
	)}, remedies...)
	sentences = append(sentences,
		"Deleting this profile removes its runtime on every machine that registered it, so agents on a machine you did not intend to touch will be affected too.")

	return map[string]any{
		"error":         strings.Join(sentences, " "),
		"code":          "runtime_profile_has_active_agents",
		"active_agents": resp,
		// The sample above is capped; this is the real number.
		"active_agent_count":      total,
		"active_agents_truncated": total > int64(len(resp)),
	}
}

// DeleteRuntimeProfile removes a profile and, in the same transaction, the
// agent_runtime instance rows registered against it. Migration 120 dropped the
// DB ON DELETE CASCADE, so this app-layer cleanup is what prevents orphaned
// runtime rows. Refuses (409) while active agents are still bound to the
// profile's runtimes. Admin-gated by the router.
func (h *Handler) DeleteRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	member, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	profileUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "profileId"), "profile id")
	if !ok {
		return
	}

	// The profile-delete cascade must run the SAME teardown the runtime-delete
	// path uses for each one: agent.runtime_id is ON DELETE RESTRICT, so an
	// agent still pointing at one of these rows would turn a bare delete into a
	// 500. Active agents are refused (409); everything else is unbound rather
	// than destroyed, exactly as in service.TeardownRuntime.
	// Guard: refuse while any active (non-archived) agent is bound to one of
	// the profile's runtimes. Keep this a 409 — the profile is the thing that
	// defines those runtimes, so the user should retire the agents or move them
	// deliberately instead of having them silently unbound in bulk.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Lock the profile before planning the cascade. Daemon registration takes
	// a conflicting KEY SHARE lock in its own transaction, so it cannot insert
	// a runtime after the plan and have that row escape deletion. If the profile
	// row is already gone, still clean up any orphaned profile_id rows.
	profile, profileErr := qtx.LockRuntimeProfileForDelete(r.Context(), db.LockRuntimeProfileForDeleteParams{
		ID:          profileUUID,
		WorkspaceID: wsUUID,
	})
	profileMissing := errors.Is(profileErr, pgx.ErrNoRows)
	if profileErr != nil && !profileMissing {
		writeError(w, http.StatusInternalServerError, "failed to load runtime profile")
		return
	}

	// Lock runtime rows in deterministic ID order. Their agent/task foreign-key
	// inserts take KEY SHARE locks, preventing dependencies from appearing after
	// the active-agent check.
	runtimeIDs, err := qtx.ListAgentRuntimeIDsByProfile(r.Context(), db.ListAgentRuntimeIDsByProfileParams{
		ProfileID:   profileUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enumerate profile runtimes")
		return
	}
	if profileMissing && len(runtimeIDs) == 0 {
		writeError(w, http.StatusNotFound, "runtime profile not found")
		return
	}
	for _, runtimeID := range runtimeIDs {
		if _, err := qtx.ListUserAgentsByRuntimeForUpdate(r.Context(), runtimeID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock profile dependencies")
			return
		}
	}

	// Bounded read: the guard only needs "is there at least one", and the
	// refusal needs a few names plus the exact total, which rides on each row.
	blockingAgents, err := qtx.ListActiveAgentsByProfile(r.Context(), db.ListActiveAgentsByProfileParams{
		ProfileID:   profileUUID,
		WorkspaceID: wsUUID,
		MaxRows:     maxReportedBlockingAgents,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check profile usage")
		return
	}
	if len(blockingAgents) > 0 {
		profileName := profile.DisplayName
		if profileMissing {
			profileName = ""
		}
		writeJSON(w, http.StatusConflict, profileDeleteBlockedByAgents(profileName, blockingAgents))
		return
	}

	// App-layer cascade, per runtime, mirroring DeleteAgentRuntime: unbind the
	// remaining (archived) agents and their task history, cancel anything still
	// in flight, and hard-delete only the system agents, so removing the runtime
	// rows below cannot destroy an agent, a conversation or a task record.
	var teardowns []service.RuntimeTeardownResult
	for _, rid := range runtimeIDs {
		teardown, err := service.TeardownRuntime(r.Context(), qtx, rid, service.RuntimeTeardownOptions{CancelNonTerminalTasks: true})
		if err != nil {
			if errors.Is(err, service.ErrRuntimeNotDrained) {
				slog.Error("runtime profile delete aborted: tasks not drained",
					"runtime_id", uuidToString(rid), "profile_id", uuidToString(profileUUID), "error", err)
				writeJSON(w, http.StatusConflict, map[string]any{
					"error": "a runtime of this profile still has tasks in flight; retry in a moment.",
					"code":  "runtime_delete_not_drained",
				})
				return
			}
			if errors.Is(err, service.ErrRuntimeWorkspaceMismatch) {
				slog.Error("runtime profile delete aborted: agent workspace mismatch",
					"runtime_id", uuidToString(rid), "profile_id", uuidToString(profileUUID), "error", err)
				writeJSON(w, http.StatusConflict, map[string]any{
					"error": "a runtime of this profile has an invalid cross-workspace agent binding.",
					"code":  "runtime_delete_workspace_mismatch",
				})
				return
			}
			slog.Error("runtime profile delete teardown failed",
				"runtime_id", uuidToString(rid), "profile_id", uuidToString(profileUUID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to unbind agents")
			return
		}
		teardowns = append(teardowns, teardown)
	}

	// Now the runtime rows have no agent references; remove them, then the
	// profile itself.
	deletedRuntimes, err := qtx.DeleteAgentRuntimesByProfile(r.Context(), db.DeleteAgentRuntimesByProfileParams{
		ProfileID:   profileUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("DeleteAgentRuntimesByProfile failed", "error", err, "profile_id", uuidToString(profileUUID))
		writeError(w, http.StatusInternalServerError, "failed to clean up runtime instances")
		return
	}
	if !profileMissing {
		if err := qtx.DeleteRuntimeProfile(r.Context(), db.DeleteRuntimeProfileParams{
			ID:          profileUUID,
			WorkspaceID: wsUUID,
		}); err != nil {
			slog.Error("DeleteRuntimeProfile failed", "error", err, "profile_id", uuidToString(profileUUID))
			writeError(w, http.StatusInternalServerError, "failed to delete runtime profile")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}
	for _, runtime := range deletedRuntimes {
		h.NotifyRuntimeGone(uuidToString(runtime.ID))
	}

	// Tell connected clients to refetch the runtime list (instances vanished),
	// and fan out the per-runtime teardown so unbound agents and cancelled
	// tasks reach subscribers the same way the runtime-delete path emits them.
	profileID := uuidToString(profileUUID)
	userID := uuidToString(member.UserID)
	for _, teardown := range teardowns {
		h.publishRuntimeTeardown(r.Context(), teardown, wsID, userID)
	}
	h.requestDaemonRuntimeProfileRefresh(wsID, profileID)
	h.publish(protocol.EventDaemonRegister, wsID, "member", userID, map[string]any{
		"deleted_runtime_profile_id": profileID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// DaemonListRuntimeProfiles serves the enabled runtime profiles for a workspace
// to a daemon. The daemon resolves each profile's command_name on PATH and
// registers an agent_runtime instance per profile it can run. Daemon-token
// gated by the router.
func (h *Handler) DaemonListRuntimeProfiles(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	profiles, err := h.Queries.ListEnabledRuntimeProfilesForWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runtime profiles")
		return
	}
	resp := make([]RuntimeProfileResponse, len(profiles))
	for i, p := range profiles {
		resp[i] = runtimeProfileToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id":     workspaceID,
		"runtime_profiles": resp,
	})
}

func (h *Handler) requestDaemonRuntimeProfileRefresh(workspaceID, profileID string) {
	if h.DaemonProfileRefresh == nil {
		return
	}
	h.DaemonProfileRefresh.NotifyRuntimeProfilesChanged(workspaceID, profileID)
}
