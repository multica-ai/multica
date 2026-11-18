package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type RuntimeConfigSummarySource string

const (
	RuntimeConfigSourceShared             RuntimeConfigSummarySource = "shared"
	RuntimeConfigSourceUserOverride       RuntimeConfigSummarySource = "user_override"
	RuntimeConfigSourceRecommendedMatch   RuntimeConfigSummarySource = "recommended_match"
	RuntimeConfigSourceDefaultUserRuntime RuntimeConfigSummarySource = "default_user_runtime"
	RuntimeConfigSourceMissingUserRuntime RuntimeConfigSummarySource = "missing_user_runtime"
)

type RuntimeConfigHintSource string

const (
	RuntimeConfigHintSourceRecommended RuntimeConfigHintSource = "recommended"
	RuntimeConfigHintSourceDefault     RuntimeConfigHintSource = "default"
	RuntimeConfigHintSourceSaved       RuntimeConfigHintSource = "saved"
)

type RuntimeConfigResolutionStatus string

const (
	RuntimeConfigResolutionReady              RuntimeConfigResolutionStatus = "ready"
	RuntimeConfigResolutionNoUserRuntime      RuntimeConfigResolutionStatus = "no_user_runtime"
	RuntimeConfigResolutionRuntimeUnavailable RuntimeConfigResolutionStatus = "runtime_unavailable"
	RuntimeConfigResolutionRuntimeOffline     RuntimeConfigResolutionStatus = "runtime_offline"
	RuntimeConfigResolutionModelUnavailable   RuntimeConfigResolutionStatus = "model_unavailable"
)

type RuntimeConfigHintAgentDetail struct {
	AgentName   string                  `json:"agent_name"`
	RuntimeName string                  `json:"runtime_name"`
	ModelLabel  string                  `json:"model_label"`
	Source      RuntimeConfigHintSource `json:"source"`
}

type RuntimeConfigHint struct {
	Kind              string                         `json:"kind"`
	ShouldShow        bool                           `json:"should_show"`
	RuntimeName       string                         `json:"runtime_name,omitempty"`
	ModelName         *string                        `json:"model_name,omitempty"`
	ModelLabel        string                         `json:"model_label,omitempty"`
	Source            RuntimeConfigHintSource        `json:"source,omitempty"`
	AffectedCount     int                            `json:"affected_count,omitempty"`
	RecommendedCount  int                            `json:"recommended_count,omitempty"`
	DefaultCount      int                            `json:"default_count,omitempty"`
	HintKey           string                         `json:"hint_key,omitempty"`
	AffectedAgents    []RuntimeConfigHintAgentDetail `json:"affected_agents,omitempty"`
	ResolutionStatus  RuntimeConfigResolutionStatus  `json:"resolution_status,omitempty"`
	ResolutionMessage string                         `json:"resolution_message,omitempty"`
}

type EffectiveRuntimeConfig struct {
	RuntimeID          pgtype.UUID
	RuntimeName        string
	Model              string
	ThinkingLevel      string
	ServiceTier        string
	RuntimeConfig      []byte
	MaxConcurrentTasks int32
	Source             RuntimeConfigSummarySource
	NoticeSeen         bool
	Hint               *RuntimeConfigHint
	ResolutionStatus   RuntimeConfigResolutionStatus
	ResolutionMessage  string
}

type RuntimeConfigSwitchFields struct {
	Model         string
	ThinkingLevel string
	ServiceTier   string
	RuntimeConfig []byte
}

// RuntimeConfigFieldsForTarget applies the personal-config runtime picker
// contract: provider-specific selections survive edits on the same runtime but
// reset to the target runtime's defaults when the runtime itself changes.
func RuntimeConfigFieldsForTarget(
	currentRuntimeID pgtype.UUID,
	targetRuntimeID pgtype.UUID,
	model string,
	thinkingLevel string,
	serviceTier string,
	runtimeConfig []byte,
) RuntimeConfigSwitchFields {
	if currentRuntimeID.Valid != targetRuntimeID.Valid || currentRuntimeID.Bytes != targetRuntimeID.Bytes {
		return RuntimeConfigSwitchFields{RuntimeConfig: []byte("{}")}
	}
	return RuntimeConfigSwitchFields{
		Model:         model,
		ThinkingLevel: thinkingLevel,
		ServiceTier:   serviceTier,
		RuntimeConfig: append([]byte(nil), runtimeConfig...),
	}
}

var (
	ErrEffectiveRuntimeMissing      = errors.New("effective runtime missing")
	ErrEffectiveRuntimeUnavailable  = errors.New("effective runtime unavailable")
	ErrEffectiveRuntimeOffline      = errors.New("effective runtime offline")
	ErrEffectiveRuntimeModelInvalid = errors.New("effective runtime model unavailable")
)

type RuntimeConfigResolutionError struct {
	Status  RuntimeConfigResolutionStatus
	Message string
	Err     error
}

func (e *RuntimeConfigResolutionError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Status)
}

func (e *RuntimeConfigResolutionError) Unwrap() error {
	return e.Err
}

func RuntimeConfigResolutionFromError(err error) (RuntimeConfigResolutionStatus, string, bool) {
	var resolutionErr *RuntimeConfigResolutionError
	if !errors.As(err, &resolutionErr) {
		return "", "", false
	}
	return resolutionErr.Status, resolutionErr.Message, true
}

func RuntimeConfigResolutionMessage(status RuntimeConfigResolutionStatus) string {
	return runtimeConfigResolutionMessage(status)
}

// RuntimeConfigDispatchReason maps the requester's effective-runtime result to
// the stable admission reason used by handlers and enqueue callers.
func RuntimeConfigDispatchReason(err error) (dispatch.ReasonCode, bool) {
	status, _, ok := RuntimeConfigResolutionFromError(err)
	if !ok {
		return "", false
	}
	switch status {
	case RuntimeConfigResolutionNoUserRuntime, RuntimeConfigResolutionRuntimeUnavailable:
		return dispatch.ReasonAgentRuntimeRequired, true
	case RuntimeConfigResolutionRuntimeOffline:
		return dispatch.ReasonRuntimeOffline, true
	case RuntimeConfigResolutionModelUnavailable:
		return dispatch.ReasonTargetUnavailable, true
	default:
		return dispatch.ReasonInternalError, true
	}
}

func (s *TaskService) ResolveEffectiveRuntimeConfig(ctx context.Context, agent db.Agent, originatorUserID pgtype.UUID) (EffectiveRuntimeConfig, error) {
	return ResolveEffectiveRuntimeConfig(ctx, s.Queries, agent, originatorUserID, s.LiveRuntimeModels)
}

// LiveRuntimeModels is the last daemon-reported model list for one runtime.
// known=false means no live catalog: the model check must fail open. The
// static Codex table is a discovery fallback, not a save/resolve allowlist.
type LiveRuntimeModels interface {
	LiveModelIDs(ctx context.Context, runtimeID string) (ids map[string]struct{}, known bool)
}

func liveCatalogForRuntime(ctx context.Context, catalog LiveRuntimeModels, runtimeID pgtype.UUID) (map[string]struct{}, bool) {
	if catalog == nil || !runtimeID.Valid {
		return nil, false
	}
	return catalog.LiveModelIDs(ctx, uuidString(runtimeID))
}

// ModelMissingFromLiveCatalog reports whether a persisted model should be
// treated as unavailable. Empty model (follow CLI default) and an unknown
// catalog fail open.
func ModelMissingFromLiveCatalog(model string, ids map[string]struct{}, known bool) bool {
	model = strings.TrimSpace(model)
	if model == "" || !known {
		return false
	}
	_, ok := ids[model]
	return !ok
}

// ClaimModelSelection is the model layer a claimed task launches with: the
// values the claim handler copies into the daemon's TaskAgentData payload.
type ClaimModelSelection struct {
	Model         string
	ThinkingLevel string
	ServiceTier   string
	RuntimeConfig []byte
}

// ResolveClaimModelSelection returns the model layer a claimed task must
// launch with, keyed on runtime_config_source the way smart-runtime defined the
// stamp. A stamped row launches with its enqueue-time effective config; a
// smart-runtime row without the stamp (pre-stamp backlog, delegated-failure
// recovery) has no authoritative model layer, and the agent's template model
// must never become one — it is only a CLI + model recommendation (smart-runtime).
// Every source is finally checked against the claiming runtime's live model
// catalog: a model that left it falls back to the originator's current user
// config, or fails the claim with an explainable
// RuntimeConfigResolutionModelUnavailable error instead of launching into a
// bare provider 400 [1211] (smart-runtime).
func ResolveClaimModelSelection(ctx context.Context, queries *db.Queries, task db.AgentTaskQueue, agent db.Agent, catalog LiveRuntimeModels) (ClaimModelSelection, error) {
	if !agent.ShareRuntime && !task.RuntimeConfigSource.Valid {
		return resolveClaimSelectionFromOriginator(ctx, queries, task, agent, catalog)
	}
	sel := ClaimModelSelection{
		Model:         textValue(task.EffectiveModel),
		ThinkingLevel: textValue(task.EffectiveThinkingLevel),
		ServiceTier:   textValue(task.EffectiveServiceTier),
		RuntimeConfig: task.EffectiveRuntimeConfig,
	}
	if !task.RuntimeConfigSource.Valid {
		// A pre-stamp shared-runtime row: the agent row is its config layer.
		sel = ClaimModelSelection{
			Model:         textValue(agent.Model),
			ThinkingLevel: textValue(agent.ThinkingLevel),
			ServiceTier:   textValue(agent.ServiceTier),
			RuntimeConfig: agent.RuntimeConfig,
		}
	}
	if !claimModelMissingFromCatalog(ctx, catalog, task.RuntimeID, sel.Model) {
		return sel, nil
	}
	if !agent.ShareRuntime {
		// The stamped model left the runtime's catalog after enqueue; the
		// originator's current user config is the fresher source.
		resolved, err := resolveClaimSelectionFromOriginator(ctx, queries, task, agent, catalog)
		if err != nil {
			return ClaimModelSelection{}, err
		}
		if !claimModelMissingFromCatalog(ctx, catalog, task.RuntimeID, resolved.Model) {
			return resolved, nil
		}
		// Keep the fresher model so the failure names what the user config
		// resolves to today, not the stale enqueue-time stamp.
		sel = resolved
	}
	return ClaimModelSelection{}, claimModelUnavailableError(sel.Model)
}

// resolveClaimSelectionFromOriginator resolves the model layer from the task
// originator's current effective runtime config — the same primitive the
// enqueue admission reads. The claiming machine is online by definition, so a
// heartbeat-lag offline verdict is tolerated exactly the way the enqueue's
// offline policy tolerates it.
func resolveClaimSelectionFromOriginator(ctx context.Context, queries *db.Queries, task db.AgentTaskQueue, agent db.Agent, catalog LiveRuntimeModels) (ClaimModelSelection, error) {
	effective, err := ResolveEffectiveRuntimeConfig(ctx, queries, agent, task.OriginatorUserID, catalog)
	if err != nil {
		status, _, resolution := RuntimeConfigResolutionFromError(err)
		if !resolution || status != RuntimeConfigResolutionRuntimeOffline || !effective.RuntimeID.Valid {
			if resolution && status == RuntimeConfigResolutionModelUnavailable && effective.Model != "" {
				err = claimModelUnavailableError(effective.Model)
			}
			return ClaimModelSelection{}, err
		}
	}
	sel := ClaimModelSelection{
		Model:         effective.Model,
		ThinkingLevel: effective.ThinkingLevel,
		ServiceTier:   effective.ServiceTier,
		RuntimeConfig: []byte("{}"),
	}
	if effective.RuntimeID.Valid && effective.RuntimeID == task.RuntimeID {
		sel.RuntimeConfig = effective.RuntimeConfig
	}
	return sel, nil
}

func claimModelMissingFromCatalog(ctx context.Context, catalog LiveRuntimeModels, runtimeID pgtype.UUID, model string) bool {
	ids, known := liveCatalogForRuntime(ctx, catalog, runtimeID)
	return ModelMissingFromLiveCatalog(model, ids, known)
}

// claimModelUnavailableError names the refused model: the generic resolution
// message alone sends the user hunting through the wrong config screen.
func claimModelUnavailableError(model string) error {
	return &RuntimeConfigResolutionError{
		Status:  RuntimeConfigResolutionModelUnavailable,
		Message: fmt.Sprintf("Model %s is no longer in this runtime's model catalog. Update the model configuration and retry.", model),
		Err:     ErrEffectiveRuntimeModelInvalid,
	}
}

// ResolveEffectiveRuntimeConfig is the query-scoped primitive shared by task
// enqueue and admission. Keeping it receiver-free lets transaction callers use
// qtx and avoids constructing partially initialized TaskService shims.
// catalogs is optional; a miss fails the model check open.
func ResolveEffectiveRuntimeConfig(ctx context.Context, queries *db.Queries, agent db.Agent, originatorUserID pgtype.UUID, catalogs ...LiveRuntimeModels) (EffectiveRuntimeConfig, error) {
	var catalog LiveRuntimeModels
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}
	return resolveEffectiveRuntimeConfig(ctx, queries, agent, originatorUserID, catalog)
}

func resolveEffectiveRuntimeConfig(ctx context.Context, queries *db.Queries, agent db.Agent, originatorUserID pgtype.UUID, catalog LiveRuntimeModels) (EffectiveRuntimeConfig, error) {
	if agent.ShareRuntime {
		return sharedRuntimeConfig(ctx, queries, agent, catalog)
	}
	// Smart-runtime agents route only to a machine owned by the originator.
	if !originatorUserID.Valid {
		return EffectiveRuntimeConfig{
			Source:            RuntimeConfigSourceMissingUserRuntime,
			ResolutionStatus:  RuntimeConfigResolutionNoUserRuntime,
			ResolutionMessage: runtimeConfigResolutionMessage(RuntimeConfigResolutionNoUserRuntime),
		}, newRuntimeConfigResolutionError(RuntimeConfigResolutionNoUserRuntime, nil)
	}

	cfg, cfgErr := queries.GetAgentUserRuntimeConfig(ctx, db.GetAgentUserRuntimeConfigParams{
		AgentID: agent.ID,
		UserID:  originatorUserID,
	})
	if cfgErr != nil && !errors.Is(cfgErr, pgx.ErrNoRows) {
		return EffectiveRuntimeConfig{}, fmt.Errorf("get user runtime config: %w", cfgErr)
	}
	if cfgErr == nil && cfg.RuntimeID.Valid {
		rt, err := queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
			ID:          cfg.RuntimeID,
			WorkspaceID: agent.WorkspaceID,
		})
		if err != nil {
			effective := withRuntimeConfigResolution(EffectiveRuntimeConfig{
				RuntimeID:          cfg.RuntimeID,
				Model:              cfg.Model,
				ThinkingLevel:      cfg.ThinkingLevel,
				ServiceTier:        cfg.ServiceTier,
				RuntimeConfig:      nonEmptyJSON(cfg.RuntimeConfig),
				MaxConcurrentTasks: coalescePositiveInt32(cfg.MaxConcurrentTasks, agent.MaxConcurrentTasks),
				Source:             RuntimeConfigSourceUserOverride,
				NoticeSeen:         cfg.NoticeSeenAt.Valid,
			}, RuntimeConfigResolutionRuntimeUnavailable)
			return effective, newRuntimeConfigResolutionError(RuntimeConfigResolutionRuntimeUnavailable, fmt.Errorf("get saved user runtime: %w", err))
		}
		effective := EffectiveRuntimeConfig{
			RuntimeID:          cfg.RuntimeID,
			RuntimeName:        rt.Name,
			Model:              cfg.Model,
			ThinkingLevel:      cfg.ThinkingLevel,
			ServiceTier:        cfg.ServiceTier,
			RuntimeConfig:      nonEmptyJSON(cfg.RuntimeConfig),
			MaxConcurrentTasks: coalescePositiveInt32(cfg.MaxConcurrentTasks, agent.MaxConcurrentTasks),
			Source:             RuntimeConfigSourceUserOverride,
			NoticeSeen:         cfg.NoticeSeenAt.Valid,
		}
		if status := validateEffectiveRuntime(ctx, catalog, agent, originatorUserID, rt, effective.Model, true); status != RuntimeConfigResolutionReady {
			effective = withRuntimeConfigResolution(effective, status)
			return effective, newRuntimeConfigResolutionError(status, nil)
		}
		effective = withRuntimeConfigResolution(effective, RuntimeConfigResolutionReady)
		effective.Hint = runtimeConfigHint(agent, effective, true)
		return effective, nil
	}

	noticeSeen := false
	if cfgErr == nil {
		noticeSeen = cfg.NoticeSeenAt.Valid
	}

	userRuntimes, err := queries.ListAgentRuntimesByOwner(ctx, db.ListAgentRuntimesByOwnerParams{
		WorkspaceID: agent.WorkspaceID,
		OwnerID:     originatorUserID,
	})
	if err != nil {
		return EffectiveRuntimeConfig{}, fmt.Errorf("list user runtimes: %w", err)
	}
	if len(userRuntimes) == 0 {
		return EffectiveRuntimeConfig{
			Source:            RuntimeConfigSourceMissingUserRuntime,
			NoticeSeen:        noticeSeen,
			ResolutionStatus:  RuntimeConfigResolutionNoUserRuntime,
			ResolutionMessage: runtimeConfigResolutionMessage(RuntimeConfigResolutionNoUserRuntime),
		}, newRuntimeConfigResolutionError(RuntimeConfigResolutionNoUserRuntime, nil)
	}

	recommendedProvider := RecommendedCLI(agent)

	if recommendedProvider != "" {
		if matched, ok := matchRecommendedCLI(userRuntimes, recommendedProvider); ok {
			effective := EffectiveRuntimeConfig{
				RuntimeID:          matched.ID,
				RuntimeName:        matched.Name,
				Model:              textValue(agent.Model),
				ThinkingLevel:      textValue(agent.ThinkingLevel),
				ServiceTier:        textValue(agent.ServiceTier),
				RuntimeConfig:      []byte("{}"),
				MaxConcurrentTasks: agent.MaxConcurrentTasks,
				Source:             RuntimeConfigSourceRecommendedMatch,
				NoticeSeen:         noticeSeen,
			}
			if status := validateEffectiveRuntime(ctx, catalog, agent, originatorUserID, matched, effective.Model, true); status != RuntimeConfigResolutionReady {
				effective = withRuntimeConfigResolution(effective, status)
				return effective, newRuntimeConfigResolutionError(status, nil)
			}
			effective = withRuntimeConfigResolution(effective, RuntimeConfigResolutionReady)
			effective.Hint = runtimeConfigHint(agent, effective, noticeSeen)
			return effective, nil
		}
	}

	return defaultUserRuntimeConfig(ctx, catalog, agent, originatorUserID, pickOriginatorRuntime(userRuntimes), noticeSeen)
}

func defaultUserRuntimeConfig(ctx context.Context, catalog LiveRuntimeModels, agent db.Agent, originatorUserID pgtype.UUID, defaultRuntime db.AgentRuntime, noticeSeen bool) (EffectiveRuntimeConfig, error) {
	effective := EffectiveRuntimeConfig{
		RuntimeID:          defaultRuntime.ID,
		RuntimeName:        defaultRuntime.Name,
		Model:              "",
		ThinkingLevel:      "",
		ServiceTier:        "",
		RuntimeConfig:      []byte("{}"),
		MaxConcurrentTasks: agent.MaxConcurrentTasks,
		Source:             RuntimeConfigSourceDefaultUserRuntime,
		NoticeSeen:         noticeSeen,
	}
	if status := validateEffectiveRuntime(ctx, catalog, agent, originatorUserID, defaultRuntime, effective.Model, true); status != RuntimeConfigResolutionReady {
		effective = withRuntimeConfigResolution(effective, status)
		return effective, newRuntimeConfigResolutionError(status, nil)
	}
	effective = withRuntimeConfigResolution(effective, RuntimeConfigResolutionReady)
	effective.Hint = runtimeConfigHint(agent, effective, noticeSeen)
	return effective, nil
}

func sharedRuntimeConfig(ctx context.Context, queries *db.Queries, agent db.Agent, catalog LiveRuntimeModels) (EffectiveRuntimeConfig, error) {
	if !agent.RuntimeID.Valid {
		effective := withRuntimeConfigResolution(EffectiveRuntimeConfig{
			Source:     RuntimeConfigSourceShared,
			NoticeSeen: true,
		}, RuntimeConfigResolutionNoUserRuntime)
		return effective, newRuntimeConfigResolutionError(RuntimeConfigResolutionNoUserRuntime, nil)
	}
	rt, err := queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          agent.RuntimeID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		effective := withRuntimeConfigResolution(EffectiveRuntimeConfig{
			RuntimeID:          agent.RuntimeID,
			Model:              textValue(agent.Model),
			ThinkingLevel:      textValue(agent.ThinkingLevel),
			ServiceTier:        textValue(agent.ServiceTier),
			RuntimeConfig:      nonEmptyJSON(agent.RuntimeConfig),
			MaxConcurrentTasks: agent.MaxConcurrentTasks,
			Source:             RuntimeConfigSourceShared,
			NoticeSeen:         true,
		}, RuntimeConfigResolutionRuntimeUnavailable)
		return effective, newRuntimeConfigResolutionError(RuntimeConfigResolutionRuntimeUnavailable, fmt.Errorf("get shared runtime: %w", err))
	}
	effective := EffectiveRuntimeConfig{
		RuntimeID:          agent.RuntimeID,
		RuntimeName:        rt.Name,
		Model:              textValue(agent.Model),
		ThinkingLevel:      textValue(agent.ThinkingLevel),
		ServiceTier:        textValue(agent.ServiceTier),
		RuntimeConfig:      nonEmptyJSON(agent.RuntimeConfig),
		MaxConcurrentTasks: agent.MaxConcurrentTasks,
		Source:             RuntimeConfigSourceShared,
		NoticeSeen:         true,
	}
	if status := validateEffectiveRuntime(ctx, catalog, agent, pgtype.UUID{}, rt, effective.Model, false); status != RuntimeConfigResolutionReady {
		effective = withRuntimeConfigResolution(effective, status)
		return effective, newRuntimeConfigResolutionError(status, nil)
	}
	return withRuntimeConfigResolution(effective, RuntimeConfigResolutionReady), nil
}

func TaskEffectiveModel(e EffectiveRuntimeConfig) pgtype.Text {
	if e.Model == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: e.Model, Valid: true}
}

func TaskEffectiveThinkingLevel(e EffectiveRuntimeConfig) pgtype.Text {
	if e.ThinkingLevel == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: e.ThinkingLevel, Valid: true}
}

func TaskEffectiveServiceTier(e EffectiveRuntimeConfig) pgtype.Text {
	if e.ServiceTier == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: e.ServiceTier, Valid: true}
}

func TaskEffectiveMaxConcurrentTasks(e EffectiveRuntimeConfig) pgtype.Int4 {
	if e.MaxConcurrentTasks <= 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: e.MaxConcurrentTasks, Valid: true}
}

func TaskRuntimeConfigSource(e EffectiveRuntimeConfig) pgtype.Text {
	if e.Source == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(e.Source), Valid: true}
}

func RuntimeConfigHintJSON(hint *RuntimeConfigHint) []byte {
	if hint == nil {
		return nil
	}
	b, err := json.Marshal(hint)
	if err != nil {
		return nil
	}
	return b
}

func DecodeRuntimeConfigHint(raw []byte) *RuntimeConfigHint {
	if len(raw) == 0 {
		return nil
	}
	var hint RuntimeConfigHint
	if err := json.Unmarshal(raw, &hint); err != nil {
		return nil
	}
	return &hint
}

func runtimeConfigHint(agent db.Agent, effective EffectiveRuntimeConfig, noticeSeen bool) *RuntimeConfigHint {
	hintSource := hintSourceForEffectiveSource(effective.Source)
	if hintSource == "" {
		return nil
	}
	modelName := stringPtrIfNotEmpty(effective.Model)
	return &RuntimeConfigHint{
		Kind:              "agent",
		ShouldShow:        shouldShowRuntimeHint(hintSource, noticeSeen),
		RuntimeName:       effective.RuntimeName,
		ModelName:         modelName,
		ModelLabel:        modelLabel(effective.Model),
		Source:            hintSource,
		ResolutionStatus:  effective.RuntimeConfigResolutionStatus(),
		ResolutionMessage: effective.ResolutionMessage,
		HintKey: fmt.Sprintf(
			"agent:%s:runtime:%s:source:%s",
			util.UUIDToString(agent.ID),
			util.UUIDToString(effective.RuntimeID),
			hintSource,
		),
	}
}

func hintSourceForEffectiveSource(source RuntimeConfigSummarySource) RuntimeConfigHintSource {
	switch source {
	case RuntimeConfigSourceUserOverride:
		return RuntimeConfigHintSourceSaved
	case RuntimeConfigSourceRecommendedMatch:
		return RuntimeConfigHintSourceRecommended
	case RuntimeConfigSourceDefaultUserRuntime:
		return RuntimeConfigHintSourceDefault
	default:
		return ""
	}
}

func shouldShowRuntimeHint(source RuntimeConfigHintSource, noticeSeen bool) bool {
	if noticeSeen {
		return false
	}
	return source == RuntimeConfigHintSourceRecommended || source == RuntimeConfigHintSourceDefault
}

// RecommendedCLI is the stored smart-runtime recommendation. Empty for fixed
// agents and for smart agents that have not picked a CLI yet.
func RecommendedCLI(agent db.Agent) string {
	return strings.ToLower(strings.TrimSpace(agent.RecommendedCli))
}

// AgentHasRuntimeBinding reports whether the Agent is configured enough to
// dispatch. Fixed agents need a bound machine; smart agents need a recommended
// CLI (or a leftover pre-migration runtime_id that still carries that CLI).
func AgentHasRuntimeBinding(agent db.Agent) bool {
	if agent.ShareRuntime {
		return agent.RuntimeID.Valid
	}
	return RecommendedCLI(agent) != ""
}

// NormalizeRecommendedCLI lowercases a CLI selector such as cursor or codex.
func NormalizeRecommendedCLI(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", false
	}
	for i, r := range s {
		if unicode.IsLetter(r) && unicode.Is(unicode.Latin, r) {
			continue
		}
		if unicode.IsDigit(r) || r == '_' || r == '-' {
			if i == 0 {
				return "", false
			}
			continue
		}
		return "", false
	}
	return s, true
}

func matchRecommendedCLI(runtimes []db.AgentRuntime, provider string) (db.AgentRuntime, bool) {
	// Recommendation is a CLI (provider) plus model, not a destination
	// machine or runtime_mode. Prefer an online match so an older offline
	// CLI does not block a later healthy one. Among online matches, prefer the
	// most recently seen machine so Autopilot/e2e heartbeats land on the
	// runtime the originator is actually using, not an older leftover in the
	// same workspace. Equal last_seen keeps created_at/list order. Never
	// prefer agent.RuntimeID.
	var firstMatch db.AgentRuntime
	found := false
	var bestOnline db.AgentRuntime
	foundOnline := false
	for _, rt := range runtimes {
		if !strings.EqualFold(rt.Provider, provider) {
			continue
		}
		if !found {
			firstMatch = rt
			found = true
		}
		if rt.Status != "online" {
			continue
		}
		if !foundOnline || runtimeSeenAfter(rt, bestOnline) {
			bestOnline = rt
			foundOnline = true
		}
	}
	if foundOnline {
		return bestOnline, true
	}
	return firstMatch, found
}

func pickOriginatorRuntime(runtimes []db.AgentRuntime) db.AgentRuntime {
	best := runtimes[0]
	for _, rt := range runtimes[1:] {
		if rt.Status == "online" && (best.Status != "online" || runtimeSeenAfter(rt, best)) {
			best = rt
		}
	}
	return best
}

func runtimeSeenAfter(a, b db.AgentRuntime) bool {
	aSeen := runtimeSeenAt(a)
	bSeen := runtimeSeenAt(b)
	return aSeen.After(bSeen)
}

func runtimeSeenAt(rt db.AgentRuntime) time.Time {
	if rt.LastSeenAt.Valid {
		return rt.LastSeenAt.Time
	}
	if rt.UpdatedAt.Valid {
		return rt.UpdatedAt.Time
	}
	if rt.CreatedAt.Valid {
		return rt.CreatedAt.Time
	}
	return time.Time{}
}

func textValue(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func modelLabel(model string) string {
	if model == "" {
		return "Default"
	}
	return model
}

func stringPtrIfNotEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func nonEmptyJSON(raw []byte) []byte {
	if len(raw) == 0 || !json.Valid(raw) {
		return []byte("{}")
	}
	return raw
}

func coalescePositiveInt32(value, fallback int32) int32 {
	if value > 0 {
		return value
	}
	return fallback
}

func (e EffectiveRuntimeConfig) RuntimeConfigResolutionStatus() RuntimeConfigResolutionStatus {
	if e.ResolutionStatus == "" {
		return RuntimeConfigResolutionReady
	}
	return e.ResolutionStatus
}

func withRuntimeConfigResolution(e EffectiveRuntimeConfig, status RuntimeConfigResolutionStatus) EffectiveRuntimeConfig {
	if status == "" {
		status = RuntimeConfigResolutionReady
	}
	e.ResolutionStatus = status
	e.ResolutionMessage = runtimeConfigResolutionMessage(status)
	return e
}

func validateEffectiveRuntime(ctx context.Context, catalog LiveRuntimeModels, agent db.Agent, originatorUserID pgtype.UUID, rt db.AgentRuntime, model string, requireOwner bool) RuntimeConfigResolutionStatus {
	if rt.WorkspaceID != agent.WorkspaceID {
		return RuntimeConfigResolutionRuntimeUnavailable
	}
	if requireOwner && (!originatorUserID.Valid || rt.OwnerID != originatorUserID) {
		return RuntimeConfigResolutionRuntimeUnavailable
	}
	if rt.Status != "online" {
		return RuntimeConfigResolutionRuntimeOffline
	}
	ids, known := liveCatalogForRuntime(ctx, catalog, rt.ID)
	if ModelMissingFromLiveCatalog(model, ids, known) {
		return RuntimeConfigResolutionModelUnavailable
	}
	return RuntimeConfigResolutionReady
}

func newRuntimeConfigResolutionError(status RuntimeConfigResolutionStatus, err error) error {
	return &RuntimeConfigResolutionError{
		Status:  status,
		Message: runtimeConfigResolutionMessage(status),
		Err:     runtimeConfigResolutionCause(status, err),
	}
}

func runtimeConfigResolutionCause(status RuntimeConfigResolutionStatus, err error) error {
	if err != nil {
		return err
	}
	switch status {
	case RuntimeConfigResolutionNoUserRuntime:
		return ErrEffectiveRuntimeMissing
	case RuntimeConfigResolutionRuntimeUnavailable:
		return ErrEffectiveRuntimeUnavailable
	case RuntimeConfigResolutionRuntimeOffline:
		return ErrEffectiveRuntimeOffline
	case RuntimeConfigResolutionModelUnavailable:
		return ErrEffectiveRuntimeModelInvalid
	default:
		return nil
	}
}

func runtimeConfigResolutionMessage(status RuntimeConfigResolutionStatus) string {
	switch status {
	case RuntimeConfigResolutionNoUserRuntime:
		return "Add a runtime first"
	case RuntimeConfigResolutionRuntimeUnavailable:
		return "Runtime unavailable"
	case RuntimeConfigResolutionRuntimeOffline:
		return "Runtime offline"
	case RuntimeConfigResolutionModelUnavailable:
		return "Runtime model unavailable"
	default:
		return ""
	}
}
