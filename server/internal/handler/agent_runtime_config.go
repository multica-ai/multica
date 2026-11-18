package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type AgentRuntimeConfigSummary struct {
	Recommended AgentRuntimeConfigRecommended `json:"recommended"`
	Current     AgentRuntimeConfigCurrent     `json:"current"`
}

type AgentRuntimeConfigRecommended struct {
	CLI                string          `json:"cli"`
	RuntimeID          string          `json:"runtime_id"`
	Model              string          `json:"model"`
	ThinkingLevel      string          `json:"thinking_level"`
	ServiceTier        string          `json:"service_tier"`
	MaxConcurrentTasks int32           `json:"max_concurrent_tasks"`
	RuntimeConfig      json.RawMessage `json:"runtime_config"`
	Editable           bool            `json:"editable"`
}

type AgentRuntimeConfigStrategy string

const (
	AgentRuntimeConfigStrategyRecommended AgentRuntimeConfigStrategy = "recommended"
	AgentRuntimeConfigStrategyCustom      AgentRuntimeConfigStrategy = "custom"
)

type AgentRuntimeConfigCurrent struct {
	RuntimeID          *string                               `json:"runtime_id"`
	Model              string                                `json:"model"`
	ThinkingLevel      string                                `json:"thinking_level"`
	ServiceTier        string                                `json:"service_tier"`
	MaxConcurrentTasks int32                                 `json:"max_concurrent_tasks"`
	RuntimeConfig      json.RawMessage                       `json:"runtime_config"`
	Strategy           AgentRuntimeConfigStrategy            `json:"strategy"`
	Source             service.RuntimeConfigSummarySource    `json:"source"`
	Persisted          bool                                  `json:"persisted"`
	Editable           bool                                  `json:"editable"`
	NoticeRequired     bool                                  `json:"notice_required"`
	ResolutionStatus   service.RuntimeConfigResolutionStatus `json:"resolution_status"`
	ResolutionMessage  string                                `json:"resolution_message,omitempty"`
}

type UpdateAgentRuntimeCurrentConfigRequest struct {
	Strategy           AgentRuntimeConfigStrategy `json:"strategy"`
	RuntimeID          string                     `json:"runtime_id"`
	Model              string                     `json:"model"`
	ThinkingLevel      string                     `json:"thinking_level"`
	ServiceTier        string                     `json:"service_tier"`
	MaxConcurrentTasks int32                      `json:"max_concurrent_tasks"`
	RuntimeConfig      json.RawMessage            `json:"runtime_config"`
}

type preparedAgentRuntimeCurrentConfig struct {
	RuntimeID          pgtype.UUID
	Model              string
	ThinkingLevel      string
	ServiceTier        string
	MaxConcurrentTasks int32
	RuntimeConfig      []byte
}

type personalRuntimeConfigWrite struct {
	AgentID pgtype.UUID
	Config  preparedAgentRuntimeCurrentConfig
}

type agentRuntimeCurrentConfigInputError struct {
	HTTPStatus       int
	Code             string
	Message          string
	ResolutionStatus service.RuntimeConfigResolutionStatus
}

func (h *Handler) attachRuntimeConfigSummaryForMember(ctx context.Context, resp *AgentResponse, a db.Agent, member db.Member) {
	summary, ok := h.buildRuntimeConfigSummary(ctx, a, member)
	if ok {
		resp.RuntimeConfigSummary = summary
		if !a.ShareRuntime {
			resp.CLI = summary.Recommended.CLI
			resp.RuntimeID = ""
			resp.RuntimeBound = service.AgentHasRuntimeBinding(a) || summary.Recommended.CLI != ""
		}
	}
}

func (h *Handler) buildRuntimeConfigSummary(ctx context.Context, a db.Agent, member db.Member) (*AgentRuntimeConfigSummary, bool) {
	cli := service.RecommendedCLI(a)
	runtimeID := uuidToString(a.RuntimeID)
	if !a.ShareRuntime {
		runtimeID = ""
		if cli == "" && a.RuntimeID.Valid {
			if rt, err := h.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
				ID:          a.RuntimeID,
				WorkspaceID: a.WorkspaceID,
			}); err == nil {
				cli = rt.Provider
			}
		}
	}
	recommended := AgentRuntimeConfigRecommended{
		CLI:                cli,
		RuntimeID:          runtimeID,
		Model:              textString(a.Model),
		ThinkingLevel:      textString(a.ThinkingLevel),
		ServiceTier:        textString(a.ServiceTier),
		MaxConcurrentTasks: a.MaxConcurrentTasks,
		RuntimeConfig:      maskedRuntimeConfig(a.RuntimeConfig),
		Editable:           memberCanManageAgent(a, member),
	}

	effective, err := h.runtimeConfigResolver().ResolveEffectiveRuntimeConfig(ctx, a, member.UserID)
	if err != nil {
		if _, _, ok := service.RuntimeConfigResolutionFromError(err); !ok {
			slog.Warn("build runtime config summary failed", "agent_id", uuidToString(a.ID), "user_id", uuidToString(member.UserID), "error", err)
			return nil, false
		}
		return h.runtimeConfigSummaryFromEffective(recommended, a, effective), true
	}

	return h.runtimeConfigSummaryFromEffective(recommended, a, effective), true
}

func (h *Handler) runtimeConfigSummaryFromEffective(recommended AgentRuntimeConfigRecommended, a db.Agent, effective service.EffectiveRuntimeConfig) *AgentRuntimeConfigSummary {
	runtimeID := uuidToString(effective.RuntimeID)
	runtimeIDPtr := &runtimeID
	if runtimeID == "" {
		runtimeIDPtr = nil
	}
	noticeRequired := false
	if effective.Hint != nil {
		noticeRequired = effective.Hint.ShouldShow
	}
	maxConcurrentTasks := effective.MaxConcurrentTasks
	if maxConcurrentTasks == 0 {
		maxConcurrentTasks = a.MaxConcurrentTasks
	}
	isShared := effective.Source == service.RuntimeConfigSourceShared
	return &AgentRuntimeConfigSummary{
		Recommended: recommended,
		Current: AgentRuntimeConfigCurrent{
			RuntimeID:          runtimeIDPtr,
			Model:              effective.Model,
			ThinkingLevel:      effective.ThinkingLevel,
			ServiceTier:        effective.ServiceTier,
			MaxConcurrentTasks: maxConcurrentTasks,
			RuntimeConfig:      maskedRuntimeConfig(effective.RuntimeConfig),
			Strategy:           runtimeConfigStrategyFromSource(effective.Source),
			Source:             effective.Source,
			Persisted:          isShared || effective.Source == service.RuntimeConfigSourceUserOverride,
			Editable:           !isShared && effective.ResolutionStatus != service.RuntimeConfigResolutionNoUserRuntime,
			NoticeRequired:     noticeRequired,
			ResolutionStatus:   effective.RuntimeConfigResolutionStatus(),
			ResolutionMessage:  effective.ResolutionMessage,
		},
	}
}

func (h *Handler) GetAgentRuntimeCurrentConfig(w http.ResponseWriter, r *http.Request) {
	a, member, ok := h.loadRuntimeConfigAgentForMember(w, r)
	if !ok {
		return
	}
	summary, ok := h.buildRuntimeConfigSummary(r.Context(), a, member)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to load runtime config")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) UpdateAgentRuntimeCurrentConfig(w http.ResponseWriter, r *http.Request) {
	a, member, ok := h.loadRuntimeConfigAgentForMember(w, r)
	if !ok {
		return
	}
	if a.ShareRuntime {
		writeError(w, http.StatusConflict, "runtime config is shared for this agent")
		return
	}

	var req UpdateAgentRuntimeCurrentConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Strategy {
	case AgentRuntimeConfigStrategyRecommended:
		tx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start transaction")
			return
		}
		defer tx.Rollback(r.Context())
		qtx := h.Queries.WithTx(tx)
		if !lockPersonalRuntimeConfigAgent(w, r, qtx, a, member) {
			return
		}
		if _, err := qtx.ClearAgentUserRuntimeConfigOverride(r.Context(), db.ClearAgentUserRuntimeConfigOverrideParams{
			AgentID: a.ID,
			UserID:  member.UserID,
		}); err != nil {
			slog.Warn("clear agent user runtime config override failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(a.ID))...)
			writeError(w, http.StatusInternalServerError, "failed to update runtime config")
			return
		}
		effective, resolveErr := service.ResolveEffectiveRuntimeConfig(r.Context(), qtx, a, member.UserID, h.liveRuntimeModels())
		if resolveErr != nil {
			if _, _, expected := service.RuntimeConfigResolutionFromError(resolveErr); !expected {
				writeError(w, http.StatusInternalServerError, "failed to resolve runtime config")
				return
			}
		}
		var repointed []db.AgentTaskQueue
		if effective.RuntimeID.Valid && effective.ResolutionStatus != service.RuntimeConfigResolutionRuntimeUnavailable {
			repointed, err = repointPersonalUnclaimedTasksToRuntime(r.Context(), qtx, effective.RuntimeID, member.UserID, []pgtype.UUID{a.ID})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to move queued tasks onto the new runtime")
				return
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit transaction")
			return
		}
		if len(repointed) > 0 && h.TaskService != nil {
			h.TaskService.NotifyRuntimeMayHaveWork(effective.RuntimeID)
		}
		summary, ok := h.buildRuntimeConfigSummary(r.Context(), a, member)
		if !ok {
			writeError(w, http.StatusInternalServerError, "failed to load runtime config")
			return
		}

		writeJSON(w, http.StatusOK, summary)
		return
	case "", AgentRuntimeConfigStrategyCustom:
		// Empty strategy is the pre-strategy contract: a PUT with runtime_id and
		// config fields saves a user override.
	default:
		writeError(w, http.StatusBadRequest, "strategy must be recommended or custom")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: a.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	effective, resolveErr := service.ResolveEffectiveRuntimeConfig(r.Context(), h.Queries, a, member.UserID, h.liveRuntimeModels())
	if resolveErr != nil {
		if _, _, expected := service.RuntimeConfigResolutionFromError(resolveErr); !expected {
			writeError(w, http.StatusInternalServerError, "failed to resolve runtime config")
			return
		}
	}
	runtimeConfig := req.RuntimeConfig
	if effective.RuntimeID.Valid == runtime.ID.Valid && effective.RuntimeID.Bytes == runtime.ID.Bytes {
		runtimeConfig = restoreMaskedRuntimeConfig(runtimeConfig, effective.RuntimeConfig)
	}
	providerFields := service.RuntimeConfigFieldsForTarget(
		effective.RuntimeID,
		runtime.ID,
		req.Model,
		req.ThinkingLevel,
		req.ServiceTier,
		runtimeConfig,
	)
	prepared, inputErr := prepareAgentRuntimeCurrentConfig(
		r.Context(),
		h.liveRuntimeModels(),
		a,
		member,
		runtime,
		providerFields.Model,
		providerFields.ThinkingLevel,
		providerFields.ServiceTier,
		req.MaxConcurrentTasks,
		json.RawMessage(providerFields.RuntimeConfig),
	)
	if inputErr != nil {
		writeAgentRuntimeCurrentConfigInputError(w, inputErr)
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	lockedRuntime, err := qtx.LockAgentRuntimeForAccessChange(r.Context(), runtime.ID)
	if err != nil || lockedRuntime.OwnerID != member.UserID || lockedRuntime.WorkspaceID != a.WorkspaceID {
		writeError(w, http.StatusConflict, "runtime ownership changed")
		return
	}
	if !lockPersonalRuntimeConfigAgent(w, r, qtx, a, member) {
		return
	}
	repointed, err := applyPersonalRuntimeCurrentConfigs(r.Context(), qtx, runtime.ID, member.UserID, []personalRuntimeConfigWrite{{
		AgentID: a.ID,
		Config:  prepared,
	}}, []pgtype.UUID{a.ID})
	if err != nil {
		slog.Warn("update agent user runtime config failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(a.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update runtime config")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}
	if len(repointed) > 0 && h.TaskService != nil {
		h.TaskService.NotifyRuntimeMayHaveWork(runtime.ID)
	}

	summary, ok := h.buildRuntimeConfigSummary(r.Context(), a, member)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to load runtime config")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func prepareAgentRuntimeCurrentConfig(
	ctx context.Context,
	catalog service.LiveRuntimeModels,
	a db.Agent,
	member db.Member,
	runtime db.AgentRuntime,
	model string,
	thinkingLevel string,
	serviceTier string,
	maxConcurrentTasks int32,
	runtimeConfig json.RawMessage,
) (preparedAgentRuntimeCurrentConfig, *agentRuntimeCurrentConfigInputError) {
	if uuidToString(runtime.OwnerID) != uuidToString(member.UserID) {
		return preparedAgentRuntimeCurrentConfig{}, &agentRuntimeCurrentConfigInputError{
			HTTPStatus: http.StatusForbidden,
			Code:       "runtime_not_owned",
			Message:    "current runtime config must use a runtime owned by the current user",
		}
	}
	if !agent.IsKnownThinkingValue(runtime.Provider, thinkingLevel) {
		return preparedAgentRuntimeCurrentConfig{}, &agentRuntimeCurrentConfigInputError{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_thinking_level",
			Message:    fmt.Sprintf("thinking_level %q is not a recognised value for runtime %q", thinkingLevel, runtime.Provider),
		}
	}
	if !agent.IsKnownServiceTier(runtime.Provider, serviceTier) {
		return preparedAgentRuntimeCurrentConfig{}, &agentRuntimeCurrentConfigInputError{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_service_tier",
			Message:    fmt.Sprintf("service_tier %q is not a recognised value for runtime %q", serviceTier, runtime.Provider),
		}
	}
	liveIDs, liveKnown := map[string]struct{}{}, false
	if catalog != nil {
		liveIDs, liveKnown = catalog.LiveModelIDs(ctx, uuidToString(runtime.ID))
	}
	if service.ModelMissingFromLiveCatalog(model, liveIDs, liveKnown) {
		return preparedAgentRuntimeCurrentConfig{}, &agentRuntimeCurrentConfigInputError{
			HTTPStatus:       http.StatusConflict,
			Code:             string(service.RuntimeConfigResolutionModelUnavailable),
			Message:          service.RuntimeConfigResolutionMessage(service.RuntimeConfigResolutionModelUnavailable),
			ResolutionStatus: service.RuntimeConfigResolutionModelUnavailable,
		}
	}
	if maxConcurrentTasks == 0 {
		maxConcurrentTasks = a.MaxConcurrentTasks
	}
	if err := validateAgentMaxConcurrentTasks(maxConcurrentTasks); err != nil {
		return preparedAgentRuntimeCurrentConfig{}, &agentRuntimeCurrentConfigInputError{HTTPStatus: http.StatusBadRequest, Code: "invalid_max_concurrent_tasks", Message: err.Error()}
	}
	normalizedRuntimeConfig := []byte("{}")
	if len(runtimeConfig) > 0 && !bytes.Equal(bytes.TrimSpace(runtimeConfig), []byte("null")) {
		if !json.Valid(runtimeConfig) {
			return preparedAgentRuntimeCurrentConfig{}, &agentRuntimeCurrentConfigInputError{
				HTTPStatus: http.StatusBadRequest,
				Code:       "invalid_runtime_config",
				Message:    "runtime_config must be valid JSON",
			}
		}
		normalizedRuntimeConfig = append([]byte(nil), runtimeConfig...)
	}
	return preparedAgentRuntimeCurrentConfig{
		RuntimeID:          runtime.ID,
		Model:              model,
		ThinkingLevel:      thinkingLevel,
		ServiceTier:        serviceTier,
		MaxConcurrentTasks: maxConcurrentTasks,
		RuntimeConfig:      normalizedRuntimeConfig,
	}, nil
}

func upsertAgentRuntimeCurrentConfig(
	ctx context.Context,
	queries *db.Queries,
	agentID pgtype.UUID,
	userID pgtype.UUID,
	prepared preparedAgentRuntimeCurrentConfig,
) error {
	_, err := queries.UpsertAgentUserRuntimeConfig(ctx, db.UpsertAgentUserRuntimeConfigParams{
		AgentID:            agentID,
		UserID:             userID,
		RuntimeID:          prepared.RuntimeID,
		Model:              prepared.Model,
		ThinkingLevel:      prepared.ThinkingLevel,
		ServiceTier:        prepared.ServiceTier,
		MaxConcurrentTasks: prepared.MaxConcurrentTasks,
		RuntimeConfig:      prepared.RuntimeConfig,
	})
	return err
}

func applyPersonalRuntimeCurrentConfigs(
	ctx context.Context,
	queries *db.Queries,
	targetRuntimeID pgtype.UUID,
	userID pgtype.UUID,
	writes []personalRuntimeConfigWrite,
	taskMigrationAgentIDs []pgtype.UUID,
) ([]db.AgentTaskQueue, error) {
	for _, write := range writes {
		if err := upsertAgentRuntimeCurrentConfig(ctx, queries, write.AgentID, userID, write.Config); err != nil {
			return nil, err
		}
	}
	return repointPersonalUnclaimedTasksToRuntime(ctx, queries, targetRuntimeID, userID, taskMigrationAgentIDs)
}

func repointPersonalUnclaimedTasksToRuntime(
	ctx context.Context,
	queries *db.Queries,
	targetRuntimeID pgtype.UUID,
	userID pgtype.UUID,
	agentIDs []pgtype.UUID,
) ([]db.AgentTaskQueue, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}
	return queries.RepointPersonalUnclaimedTasksToRuntime(ctx, db.RepointPersonalUnclaimedTasksToRuntimeParams{
		ToRuntimeID: targetRuntimeID,
		AgentIds:    agentIDs,
		UserID:      userID,
	})
}

func writeAgentRuntimeCurrentConfigInputError(w http.ResponseWriter, inputErr *agentRuntimeCurrentConfigInputError) {
	if inputErr.ResolutionStatus != "" {
		writeRuntimeConfigResolutionStatus(w, inputErr.ResolutionStatus)
		return
	}
	writeError(w, inputErr.HTTPStatus, inputErr.Message)
}

func maskedRuntimeConfig(raw []byte) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return json.RawMessage(`{}`)
	}
	maskGatewayToken(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(encoded)
}

func restoreMaskedRuntimeConfig(raw json.RawMessage, persisted []byte) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	preserveMaskedGatewayToken(value, persisted)
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return json.RawMessage(encoded)
}

func (h *Handler) MarkAgentRuntimeConfigNoticeSeen(w http.ResponseWriter, r *http.Request) {
	a, member, ok := h.loadRuntimeConfigAgentForMember(w, r)
	if !ok {
		return
	}
	if !a.ShareRuntime {
		if _, err := h.Queries.MarkAgentUserRuntimeConfigNoticeSeen(r.Context(), db.MarkAgentUserRuntimeConfigNoticeSeenParams{
			AgentID: a.ID,
			UserID:  member.UserID,
		}); err != nil {
			slog.Warn("mark runtime config notice seen failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(a.ID))...)
			writeError(w, http.StatusInternalServerError, "failed to update runtime config notice")
			return
		}
	}
	summary, ok := h.buildRuntimeConfigSummary(r.Context(), a, member)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to load runtime config")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) loadRuntimeConfigAgentForMember(w http.ResponseWriter, r *http.Request) (db.Agent, db.Member, bool) {
	id := chi.URLParam(r, "id")
	a, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return db.Agent{}, db.Member{}, false
	}
	member, ok := h.workspaceMember(w, r, uuidToString(a.WorkspaceID))
	if !ok {
		return db.Agent{}, db.Member{}, false
	}
	if !h.canAccessPrivateAgent(r.Context(), a, "member", uuidToString(member.UserID), uuidToString(a.WorkspaceID)) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return db.Agent{}, db.Member{}, false
	}
	return a, member, true
}

func (h *Handler) runtimeConfigResolver() *service.TaskService {
	catalog := h.liveRuntimeModels()
	if h.TaskService != nil {
		return &service.TaskService{Queries: h.TaskService.Queries, LiveRuntimeModels: catalog}
	}
	return &service.TaskService{Queries: h.Queries, LiveRuntimeModels: catalog}
}

func (h *Handler) resolveEffectiveAgentRuntime(ctx context.Context, source string, agent db.Agent, userID pgtype.UUID) (db.AgentRuntime, error) {
	effective, err := h.runtimeConfigResolver().ResolveEffectiveRuntimeConfig(ctx, agent, userID)
	if err != nil {
		return db.AgentRuntime{}, err
	}
	return h.runtimeLookup(source).GetForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{ID: effective.RuntimeID, WorkspaceID: agent.WorkspaceID})
}

func memberCanManageAgent(a db.Agent, member db.Member) bool {
	return roleAllowed(member.Role, "owner", "admin") || a.OwnerID == member.UserID
}

func runtimeConfigStrategyFromSource(source service.RuntimeConfigSummarySource) AgentRuntimeConfigStrategy {
	if source == service.RuntimeConfigSourceUserOverride {
		return AgentRuntimeConfigStrategyCustom
	}
	return AgentRuntimeConfigStrategyRecommended
}

func textString(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func runtimeConfigHintFromTask(task db.AgentTaskQueue) *service.RuntimeConfigHint {
	return service.DecodeRuntimeConfigHint(task.RuntimeConfigHint)
}

func writeRuntimeConfigResolutionHTTPError(w http.ResponseWriter, err error) bool {
	status, message, ok := service.RuntimeConfigResolutionFromError(err)
	if !ok {
		return false
	}
	if message == "" {
		message = service.RuntimeConfigResolutionMessage(status)
	}
	payload := map[string]any{
		"code":               string(status),
		"error":              message,
		"resolution_status":  status,
		"resolution_message": message,
	}
	if reason, mapped := service.RuntimeConfigDispatchReason(err); mapped {
		payload["reason_code"] = reason
	}
	writeJSON(w, http.StatusConflict, payload)
	return true
}

func writeRuntimeConfigResolutionStatus(w http.ResponseWriter, status service.RuntimeConfigResolutionStatus) {
	message := service.RuntimeConfigResolutionMessage(status)
	writeJSON(w, http.StatusConflict, map[string]any{
		"code":               string(status),
		"error":              message,
		"resolution_status":  status,
		"resolution_message": message,
	})
}

func appendRuntimeConfigHint(acc, next *service.RuntimeConfigHint) *service.RuntimeConfigHint {
	if next == nil {
		return acc
	}
	if acc == nil {
		return next
	}
	aggregate := normalizeRuntimeConfigHintAggregate(acc)
	mergeRuntimeConfigHintIntoAggregate(aggregate, next)
	return aggregate
}

func normalizeRuntimeConfigHintAggregate(hint *service.RuntimeConfigHint) *service.RuntimeConfigHint {
	if hint.Kind == "squad" {
		if hint.ResolutionStatus == "" {
			hint.ResolutionStatus = service.RuntimeConfigResolutionReady
		}
		return hint
	}
	aggregate := &service.RuntimeConfigHint{Kind: "squad", ResolutionStatus: service.RuntimeConfigResolutionReady}
	mergeRuntimeConfigHintIntoAggregate(aggregate, hint)
	return aggregate
}

func mergeRuntimeConfigHintIntoAggregate(aggregate, hint *service.RuntimeConfigHint) {
	if aggregate.ResolutionStatus == "" {
		aggregate.ResolutionStatus = service.RuntimeConfigResolutionReady
	}
	if hint.Kind == "squad" {
		aggregate.ShouldShow = aggregate.ShouldShow || hint.ShouldShow
		aggregate.AffectedCount += hint.AffectedCount
		aggregate.RecommendedCount += hint.RecommendedCount
		aggregate.DefaultCount += hint.DefaultCount
		if aggregate.HintKey == "" {
			aggregate.HintKey = hint.HintKey
		}
		return
	}
	aggregate.ShouldShow = aggregate.ShouldShow || hint.ShouldShow
	aggregate.AffectedCount++
	switch hint.Source {
	case service.RuntimeConfigHintSourceRecommended:
		aggregate.RecommendedCount++
	case service.RuntimeConfigHintSourceDefault:
		aggregate.DefaultCount++
	}
	if aggregate.HintKey == "" {
		aggregate.HintKey = "squad:runtime-config"
	}
}

// Lock the agent before writing the FK-free config row, then recheck membership.
func lockPersonalRuntimeConfigAgent(w http.ResponseWriter, r *http.Request, q *db.Queries, agent db.Agent, member db.Member) bool {
	locked, err := q.GetAgentForUpdate(r.Context(), agent.ID)
	if err != nil || locked.WorkspaceID != agent.WorkspaceID || locked.ArchivedAt.Valid || locked.ShareRuntime {
		writeError(w, http.StatusConflict, "agent runtime strategy changed")
		return false
	}
	if _, err := q.LockActiveMember(r.Context(), db.LockActiveMemberParams{WorkspaceID: agent.WorkspaceID, UserID: member.UserID}); err != nil {
		writeError(w, http.StatusForbidden, "workspace membership is required")
		return false
	}
	return true
}
