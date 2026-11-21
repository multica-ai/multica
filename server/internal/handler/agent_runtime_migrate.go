package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const maxMigratePersonalRuntimeAgentsBatch = 200

const (
	migrateRuntimeSkipNotFound  = "not_found"
	migrateRuntimeSkipForbidden = "forbidden"
)

type migrateRuntimeConfigScope string

const (
	migrateRuntimeConfigScopePersonal migrateRuntimeConfigScope = "personal"
	migrateRuntimeConfigScopeAgent    migrateRuntimeConfigScope = "agent"
)

type migrateAgentsToRuntimeRequest struct {
	AgentIDs           []string        `json:"agent_ids"`
	DryRun             bool            `json:"dry_run"`
	Model              string          `json:"model"`
	ThinkingLevel      string          `json:"thinking_level"`
	ServiceTier        string          `json:"service_tier"`
	MaxConcurrentTasks int32           `json:"max_concurrent_tasks"`
	RuntimeConfig      json.RawMessage `json:"runtime_config"`
}

type migratedAgentRuntimeCurrentConfig struct {
	RuntimeID          string                                `json:"runtime_id"`
	Model              string                                `json:"model"`
	ThinkingLevel      string                                `json:"thinking_level"`
	ServiceTier        string                                `json:"service_tier"`
	MaxConcurrentTasks int32                                 `json:"max_concurrent_tasks"`
	RuntimeConfig      json.RawMessage                       `json:"runtime_config"`
	Strategy           AgentRuntimeConfigStrategy            `json:"strategy"`
	Source             service.RuntimeConfigSummarySource    `json:"source"`
	ResolutionStatus   service.RuntimeConfigResolutionStatus `json:"resolution_status"`
	ResolutionMessage  string                                `json:"resolution_message,omitempty"`
}

type migratedAgentRuntimeResult struct {
	AgentID           string                            `json:"agent_id"`
	Name              string                            `json:"name"`
	PreviousRuntimeID *string                           `json:"previous_runtime_id"`
	CurrentConfig     migratedAgentRuntimeCurrentConfig `json:"current_config"`
	ConfigScope       migrateRuntimeConfigScope         `json:"config_scope"`
	prepared          preparedAgentRuntimeCurrentConfig `json:"-"`
	agent             db.Agent                          `json:"-"`
}

type skippedAgentRuntimeResult struct {
	AgentID           string                                `json:"agent_id"`
	Name              string                                `json:"name,omitempty"`
	Reason            string                                `json:"reason"`
	ResolutionStatus  service.RuntimeConfigResolutionStatus `json:"resolution_status,omitempty"`
	ResolutionMessage string                                `json:"resolution_message,omitempty"`
}

type migrateAgentsToRuntimeResponse struct {
	TargetRuntimeID     string                       `json:"target_runtime_id"`
	TargetRuntimeStatus string                       `json:"target_runtime_status"`
	DryRun              bool                         `json:"dry_run"`
	Migrated            []migratedAgentRuntimeResult `json:"migrated"`
	Skipped             []skippedAgentRuntimeResult  `json:"skipped"`
	TasksMigrated       int64                        `json:"tasks_migrated"`
	TasksStayingActive  int64                        `json:"tasks_staying_active"`
}

type personalRuntimeMigrationPlan struct {
	// Config IDs include same-runtime updates. Task IDs only include agents whose
	// effective runtime changes, so configuration-only updates never move work.
	personalIDs       []pgtype.UUID
	personalTaskIDs   []pgtype.UUID
	agentLevelIDs     []pgtype.UUID
	agentLevelTaskIDs []pgtype.UUID
	migrated          []migratedAgentRuntimeResult
	skipped           []skippedAgentRuntimeResult
}

// MigrateAgentsToRuntime previews or applies one selected runtime configuration
// to multiple agents. Smart-runtime agents keep their per-user override while
// ordinary agents owned by the caller update their shared Agent-level config.
func (h *Handler) MigrateAgentsToRuntime(w http.ResponseWriter, r *http.Request) {
	targetID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "runtimeId"), "runtime_id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	target, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          targetID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	if uuidToString(target.OwnerID) != uuidToString(member.UserID) {
		writeError(w, http.StatusForbidden, "current runtime config must use a runtime owned by the current user")
		return
	}

	var req migrateAgentsToRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentIDs, ok := parsePersonalRuntimeMigrationAgentIDs(w, req.AgentIDs)
	if !ok {
		return
	}
	if err := validateUniformRuntimeMigrationConfig(r.Context(), h.liveRuntimeModels(), target, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.DryRun {
		plan, err := h.planPersonalRuntimeMigration(r.Context(), h.Queries, target, member, agentIDs, req)
		if err != nil {
			h.writePersonalRuntimeMigrationPlanError(w, r, err)
			return
		}
		out, err := migrationResponseWithTaskCounts(r.Context(), h.Queries, target, member.UserID, plan, true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to inspect agent tasks")
			return
		}
		slog.Info("agent runtime migration previewed",
			append(logger.RequestAttrs(r),
				"runtime_id", uuidToString(target.ID),
				"workspace_id", workspaceID,
				"originator_user_id", uuidToString(member.UserID),
				"executing_agent_id", r.Header.Get("X-Agent-ID"),
				"target_agent_ids", req.AgentIDs,
				"migrated", len(plan.migrated),
				"skipped", len(plan.skipped),
				"tasks_migrated", out.TasksMigrated,
				"operation", "migrate_agents_preview",
				"result", "previewed")...)
		writeJSON(w, http.StatusOK, out)
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	lockedTarget, err := qtx.LockAgentRuntimeForAccessChange(r.Context(), target.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "runtime not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock runtime")
		return
	}
	if uuidToString(lockedTarget.WorkspaceID) != uuidToString(target.WorkspaceID) ||
		uuidToString(lockedTarget.OwnerID) != uuidToString(member.UserID) {
		writeError(w, http.StatusForbidden, "current runtime config must use a runtime owned by the current user")
		return
	}

	agents, err := qtx.ListAgentsByIDsForWorkspaceForUpdate(r.Context(), db.ListAgentsByIDsForWorkspaceForUpdateParams{
		AgentIds:    agentIDs,
		WorkspaceID: lockedTarget.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agents")
		return
	}
	workspaceAgentIDs := make([]pgtype.UUID, 0, len(agents))
	for _, a := range agents {
		workspaceAgentIDs = append(workspaceAgentIDs, a.ID)
	}
	if _, err := qtx.LockAgentUserRuntimeConfigsForUpdate(r.Context(), db.LockAgentUserRuntimeConfigsForUpdateParams{
		AgentIds: workspaceAgentIDs,
		UserID:   member.UserID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock runtime configs")
		return
	}
	plan, err := h.planPersonalRuntimeMigrationFromAgents(r.Context(), qtx, lockedTarget, member, agentIDs, agents, req)
	if err != nil {
		h.writePersonalRuntimeMigrationPlanError(w, r, err)
		return
	}
	out, err := migrationResponseWithTaskCounts(r.Context(), qtx, lockedTarget, member.UserID, plan, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect agent tasks")
		return
	}

	writeOrder := append([]migratedAgentRuntimeResult(nil), plan.migrated...)
	sort.Slice(writeOrder, func(i, j int) bool { return writeOrder[i].AgentID < writeOrder[j].AgentID })
	writes := make([]personalRuntimeConfigWrite, 0, len(plan.personalIDs))
	for _, result := range writeOrder {
		if result.ConfigScope == migrateRuntimeConfigScopePersonal {
			writes = append(writes, personalRuntimeConfigWrite{AgentID: result.agent.ID, Config: result.prepared})
		}
	}

	personalRepointed, err := applyPersonalRuntimeCurrentConfigs(r.Context(), qtx, lockedTarget.ID, member.UserID, writes, plan.personalTaskIDs)
	if err != nil {
		slog.Warn("migrate personal runtime config: apply failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update runtime config")
		return
	}
	updatedAgentLevel := []db.Agent{}
	agentLevelRepointed := []db.AgentTaskQueue{}
	agentLevelRetained := []db.AgentTaskQueue{}
	if len(plan.agentLevelIDs) > 0 {
		updatedAgentLevel, err = qtx.MigrateAgentLevelRuntimeConfigs(r.Context(), db.MigrateAgentLevelRuntimeConfigsParams{
			RuntimeID:          lockedTarget.ID,
			RuntimeMode:        lockedTarget.RuntimeMode,
			Model:              pgtype.Text{String: req.Model, Valid: true},
			ThinkingLevel:      req.ThinkingLevel,
			ServiceTier:        req.ServiceTier,
			MaxConcurrentTasks: req.MaxConcurrentTasks,
			RuntimeConfig:      migrationRuntimeConfigWriteValue(req.RuntimeConfig),
			AgentIds:           plan.agentLevelIDs,
		})
		if err != nil {
			slog.Warn("migrate agent-level runtime config: apply failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to update agent runtime config")
			return
		}
		if len(plan.agentLevelTaskIDs) > 0 {
			agentLevelRetained, err = qtx.MarkAgentLevelTasksRetainedOnRuntime(r.Context(), db.MarkAgentLevelTasksRetainedOnRuntimeParams{
				ToRuntimeID: lockedTarget.ID,
				AgentIds:    plan.agentLevelTaskIDs,
				UserID:      member.UserID,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to preserve tasks on their original runtime")
				return
			}
			agentLevelRepointed, err = qtx.RepointAgentLevelUnclaimedTasksToRuntime(r.Context(), db.RepointAgentLevelUnclaimedTasksToRuntimeParams{
				ToRuntimeID: lockedTarget.ID,
				AgentIds:    plan.agentLevelTaskIDs,
				UserID:      member.UserID,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to move queued tasks onto the new runtime")
				return
			}
		}
	}
	out.TasksMigrated = int64(len(personalRepointed) + len(agentLevelRepointed))

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	for _, updated := range updatedAgentLevel {
		h.publish(protocol.EventAgentStatus, workspaceID, actorType, actorID, map[string]any{
			"agent": broadcastAgentResponse(h.agentToResponse(updated)),
		})
	}
	if h.TaskService != nil {
		runtimesWithWork := make(map[[16]byte]pgtype.UUID)
		if out.TasksMigrated > 0 {
			runtimesWithWork[lockedTarget.ID.Bytes] = lockedTarget.ID
		}
		for _, task := range agentLevelRetained {
			runtimesWithWork[task.RuntimeID.Bytes] = task.RuntimeID
		}
		for _, runtimeID := range runtimesWithWork {
			h.TaskService.NotifyRuntimeMayHaveWork(runtimeID)
		}
	}

	slog.Info("agent runtime configs migrated",
		append(logger.RequestAttrs(r),
			"runtime_id", uuidToString(lockedTarget.ID),
			"workspace_id", uuidToString(lockedTarget.WorkspaceID),
			"originator_user_id", uuidToString(member.UserID),
			"executing_agent_id", r.Header.Get("X-Agent-ID"),
			"target_agent_ids", req.AgentIDs,
			"migrated", len(plan.migrated),
			"skipped", len(plan.skipped),
			"personal_configs", len(plan.personalIDs),
			"agent_configs", len(plan.agentLevelIDs),
			"tasks_migrated", out.TasksMigrated,
			"operation", "migrate_agents_apply",
			"result", "applied")...)

	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) planPersonalRuntimeMigration(
	ctx context.Context,
	queries *db.Queries,
	target db.AgentRuntime,
	member db.Member,
	requested []pgtype.UUID,
	req migrateAgentsToRuntimeRequest,
) (personalRuntimeMigrationPlan, error) {
	agents, err := queries.ListAgentsByIDsForWorkspace(ctx, db.ListAgentsByIDsForWorkspaceParams{
		AgentIds:    requested,
		WorkspaceID: target.WorkspaceID,
	})
	if err != nil {
		return personalRuntimeMigrationPlan{}, err
	}
	return h.planPersonalRuntimeMigrationFromAgents(ctx, queries, target, member, requested, agents, req)
}

func (h *Handler) planPersonalRuntimeMigrationFromAgents(
	ctx context.Context,
	queries *db.Queries,
	target db.AgentRuntime,
	member db.Member,
	requested []pgtype.UUID,
	agents []db.Agent,
	req migrateAgentsToRuntimeRequest,
) (personalRuntimeMigrationPlan, error) {
	agentIDs := make([]pgtype.UUID, 0, len(agents))
	byID := make(map[string]db.Agent, len(agents))
	for _, a := range agents {
		agentIDs = append(agentIDs, a.ID)
		byID[uuidToString(a.ID)] = a
	}
	targetRows, err := queries.ListAgentInvocationTargetsByAgentIDs(ctx, agentIDs)
	if err != nil {
		return personalRuntimeMigrationPlan{}, err
	}
	targetsByAgent := make(map[string][]db.AgentInvocationTarget, len(agents))
	for _, invocationTarget := range targetRows {
		key := uuidToString(invocationTarget.AgentID)
		targetsByAgent[key] = append(targetsByAgent[key], invocationTarget)
	}

	plan := personalRuntimeMigrationPlan{
		migrated: []migratedAgentRuntimeResult{},
		skipped:  []skippedAgentRuntimeResult{},
	}
	userID := uuidToString(member.UserID)
	for _, requestedID := range requested {
		key := uuidToString(requestedID)
		a, found := byID[key]
		if found && !memberAllowedToViewAgent(a, targetsByAgent[key], userID, member.Role) {
			found = false
		}
		if !found {
			plan.skipped = append(plan.skipped, skippedAgentRuntimeResult{AgentID: key, Reason: migrateRuntimeSkipNotFound})
			continue
		}

		configScope := migrateRuntimeConfigScopePersonal
		source := service.RuntimeConfigSourceUserOverride
		previousRuntime := a.RuntimeID
		previousRuntimeConfig := a.RuntimeConfig
		if a.ShareRuntime {
			configScope = migrateRuntimeConfigScopeAgent
			source = service.RuntimeConfigSourceShared
			if !memberCanManageAgent(a, member) || !service.RuntimeAllowsUser(target, a.OwnerID) {
				plan.skipped = append(plan.skipped, skippedAgentRuntimeResult{AgentID: key, Name: a.Name, Reason: migrateRuntimeSkipForbidden})
				continue
			}
		} else {
			effective, resolveErr := service.ResolveEffectiveRuntimeConfig(ctx, queries, a, member.UserID, h.liveRuntimeModels())
			if resolveErr != nil {
				if _, _, expected := service.RuntimeConfigResolutionFromError(resolveErr); !expected {
					return personalRuntimeMigrationPlan{}, resolveErr
				}
			}
			previousRuntime = effective.RuntimeID
			previousRuntimeConfig = effective.RuntimeConfig
		}
		runtimeChanged := !previousRuntime.Valid || previousRuntime.Bytes != target.ID.Bytes
		runtimeConfig := req.RuntimeConfig
		if !migrationRuntimeConfigProvided(runtimeConfig) {
			runtimeConfig = json.RawMessage(previousRuntimeConfig)
		}

		prepared, inputErr := prepareAgentRuntimeCurrentConfig(
			ctx,
			h.liveRuntimeModels(),
			a,
			member,
			target,
			req.Model,
			req.ThinkingLevel,
			req.ServiceTier,
			req.MaxConcurrentTasks,
			runtimeConfig,
		)
		if inputErr != nil {
			plan.skipped = append(plan.skipped, skippedAgentRuntimeResult{
				AgentID:           key,
				Name:              a.Name,
				Reason:            inputErr.Code,
				ResolutionStatus:  inputErr.ResolutionStatus,
				ResolutionMessage: inputErr.Message,
			})
			continue
		}

		var previousRuntimeID *string
		if previousRuntime.Valid {
			value := uuidToString(previousRuntime)
			previousRuntimeID = &value
		}
		resolutionStatus := service.RuntimeConfigResolutionReady
		if target.Status != "online" {
			resolutionStatus = service.RuntimeConfigResolutionRuntimeOffline
		}
		result := migratedAgentRuntimeResult{
			AgentID:           key,
			Name:              a.Name,
			PreviousRuntimeID: previousRuntimeID,
			CurrentConfig: migratedAgentRuntimeCurrentConfig{
				RuntimeID:          uuidToString(target.ID),
				Model:              prepared.Model,
				ThinkingLevel:      prepared.ThinkingLevel,
				ServiceTier:        prepared.ServiceTier,
				MaxConcurrentTasks: prepared.MaxConcurrentTasks,
				RuntimeConfig:      maskedRuntimeConfig(prepared.RuntimeConfig),
				Strategy:           AgentRuntimeConfigStrategyCustom,
				Source:             source,
				ResolutionStatus:   resolutionStatus,
				ResolutionMessage:  service.RuntimeConfigResolutionMessage(resolutionStatus),
			},
			ConfigScope: configScope,
			prepared:    prepared,
			agent:       a,
		}
		plan.migrated = append(plan.migrated, result)
		if configScope == migrateRuntimeConfigScopePersonal {
			plan.personalIDs = append(plan.personalIDs, a.ID)
			if runtimeChanged {
				plan.personalTaskIDs = append(plan.personalTaskIDs, a.ID)
			}
		} else {
			plan.agentLevelIDs = append(plan.agentLevelIDs, a.ID)
			if runtimeChanged {
				plan.agentLevelTaskIDs = append(plan.agentLevelTaskIDs, a.ID)
			}
		}
	}
	return plan, nil
}

func migrationResponseWithTaskCounts(
	ctx context.Context,
	queries *db.Queries,
	target db.AgentRuntime,
	userID pgtype.UUID,
	plan personalRuntimeMigrationPlan,
	dryRun bool,
) (migrateAgentsToRuntimeResponse, error) {
	out := migrateAgentsToRuntimeResponse{
		TargetRuntimeID:     uuidToString(target.ID),
		TargetRuntimeStatus: target.Status,
		DryRun:              dryRun,
		Migrated:            plan.migrated,
		Skipped:             plan.skipped,
	}
	if len(plan.personalIDs) == 0 && len(plan.agentLevelIDs) == 0 {
		return out, nil
	}
	if len(plan.personalTaskIDs) > 0 {
		counts, err := queries.CountPersonalAgentTasksByMigrationGroup(ctx, db.CountPersonalAgentTasksByMigrationGroupParams{
			ToRuntimeID: target.ID,
			AgentIds:    plan.personalTaskIDs,
			UserID:      userID,
		})
		if err != nil {
			return migrateAgentsToRuntimeResponse{}, err
		}
		out.TasksMigrated += counts.UnclaimedCount
		out.TasksStayingActive += counts.ActiveCount
	}
	if len(plan.agentLevelTaskIDs) > 0 {
		counts, err := queries.CountAgentLevelTasksByMigrationGroup(ctx, db.CountAgentLevelTasksByMigrationGroupParams{
			ToRuntimeID: target.ID,
			AgentIds:    plan.agentLevelTaskIDs,
			UserID:      userID,
		})
		if err != nil {
			return migrateAgentsToRuntimeResponse{}, err
		}
		out.TasksMigrated += counts.UnclaimedCount
		out.TasksStayingActive += counts.ActiveCount
	}
	return out, nil
}

func validateUniformRuntimeMigrationConfig(ctx context.Context, catalog service.LiveRuntimeModels, target db.AgentRuntime, req migrateAgentsToRuntimeRequest) error {
	if err := validateAgentMaxConcurrentTasks(req.MaxConcurrentTasks); err != nil {
		return err
	}
	if !agentpkg.IsKnownThinkingValue(target.Provider, req.ThinkingLevel) {
		return fmt.Errorf("thinking_level %q is not a recognised value for runtime %q", req.ThinkingLevel, target.Provider)
	}
	if !agentpkg.IsKnownServiceTier(target.Provider, req.ServiceTier) {
		return fmt.Errorf("service_tier %q is not a recognised value for runtime %q", req.ServiceTier, target.Provider)
	}
	liveIDs, liveKnown := map[string]struct{}{}, false
	if catalog != nil {
		liveIDs, liveKnown = catalog.LiveModelIDs(ctx, uuidToString(target.ID))
	}
	if service.ModelMissingFromLiveCatalog(req.Model, liveIDs, liveKnown) {
		return fmt.Errorf("model %q is not available for runtime %q", req.Model, target.Provider)
	}
	runtimeConfig := bytes.TrimSpace(req.RuntimeConfig)
	if len(runtimeConfig) > 0 && !bytes.Equal(runtimeConfig, []byte("null")) && !json.Valid(runtimeConfig) {
		return fmt.Errorf("runtime_config must be valid JSON")
	}
	return nil
}

func migrationRuntimeConfigProvided(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func migrationRuntimeConfigWriteValue(raw json.RawMessage) []byte {
	if !migrationRuntimeConfigProvided(raw) {
		return nil
	}
	return append([]byte(nil), raw...)
}

func parsePersonalRuntimeMigrationAgentIDs(w http.ResponseWriter, raw []string) ([]pgtype.UUID, bool) {
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "agent_ids must not be empty")
		return nil, false
	}
	if len(raw) > maxMigratePersonalRuntimeAgentsBatch {
		writeError(w, http.StatusBadRequest, "too many agents in one request")
		return nil, false
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]pgtype.UUID, 0, len(raw))
	for _, value := range raw {
		parsed, ok := parseUUIDOrBadRequest(w, value, "agent_ids")
		if !ok {
			return nil, false
		}
		key := uuidToString(parsed)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, parsed)
	}
	return out, true
}

func (h *Handler) writePersonalRuntimeMigrationPlanError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Warn("plan personal runtime migration failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "failed to plan runtime migration")
}
