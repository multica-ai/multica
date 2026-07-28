package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agentroute"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	adaptiveRoutingDecisionVersion = 1
	adaptiveCapacityMaxAge         = 30 * time.Minute
	adaptiveRuntimeMaxAge          = 3 * time.Minute
	adaptiveFutureSkew             = 5 * time.Minute
	adaptiveAdmissionRetry         = 5 * time.Minute
	adaptivePendingSweepMinAge     = 5 * time.Second
	adaptivePendingSweepBatch      = 100
)

type adaptiveRoutingEnvelope struct {
	ProviderFailoverProtected bool                  `json:"provider_failover_protected"`
	AdaptiveRouting           adaptiveRoutingConfig `json:"adaptive_routing"`
}

type adaptiveRoutingConfig struct {
	Enabled           bool                       `json:"enabled"`
	Protected         bool                       `json:"protected"`
	Risk              agentroute.Risk            `json:"risk"`
	RequiredSkills    []string                   `json:"required_skills"`
	RequiredTools     []string                   `json:"required_tools"`
	RequiredAuthority []string                   `json:"required_authority"`
	Candidates        []adaptiveCandidateConfig  `json:"candidates"`
	Affinities        []agentroute.SkillAffinity `json:"affinities"`
}

type adaptiveCandidateConfig struct {
	ID                  string          `json:"id"`
	RuntimeID           string          `json:"runtime_id"`
	Model               string          `json:"model"`
	ThinkingLevel       string          `json:"thinking_level"`
	ServiceTier         string          `json:"service_tier"`
	QualityBP           int             `json:"quality_bp"`
	LatencyPenaltyBP    int             `json:"latency_penalty_bp"`
	ExpectedUsePermille int             `json:"expected_use_permille"`
	SupportedSkills     []string        `json:"supported_skills"`
	SupportedTools      []string        `json:"supported_tools"`
	AuthorityScopes     []string        `json:"authority_scopes"`
	RuntimeConfig       json.RawMessage `json:"runtime_config"`
	CustomArgs          []string        `json:"custom_args"`
}

type adaptiveResolvedCandidate struct {
	Config  adaptiveCandidateConfig
	Runtime db.AgentRuntime
}

type adaptiveAdmissionRecord struct {
	SchemaVersion        int                       `json:"schema_version"`
	Mode                 agentroute.Mode           `json:"mode"`
	Outcome              string                    `json:"outcome"`
	Reason               string                    `json:"reason,omitempty"`
	Selected             *adaptiveSelectedRecord   `json:"selected,omitempty"`
	Topology             agentroute.Topology       `json:"topology,omitempty"`
	Fallbacks            []adaptiveSelectedRecord  `json:"fallbacks,omitempty"`
	Rejections           []adaptiveRejectionRecord `json:"rejections,omitempty"`
	ValidationRejections []adaptiveRejectionRecord `json:"validation_rejections,omitempty"`
	Capacities           []adaptiveCapacityRecord  `json:"capacities,omitempty"`
	EvaluatedAt          time.Time                 `json:"evaluated_at"`
	RetryAt              *time.Time                `json:"retry_at,omitempty"`
}

type adaptiveSelectedRecord struct {
	CandidateID                string `json:"candidate_id"`
	Provider                   string `json:"provider"`
	RuntimeID                  string `json:"runtime_id"`
	Model                      string `json:"model"`
	ThinkingLevel              string `json:"thinking_level,omitempty"`
	ExpectedUsePermille        int    `json:"expected_use_permille"`
	ProjectedRemainingPermille int    `json:"projected_remaining_permille"`
	ProjectedHeadroomPermille  int    `json:"projected_headroom_permille"`
	Score                      int    `json:"score"`
}

type adaptiveRejectionRecord struct {
	CandidateID string `json:"candidate_id"`
	Reason      string `json:"reason"`
	Detail      string `json:"detail,omitempty"`
}

type adaptiveCapacityRecord struct {
	Provider                 string     `json:"provider"`
	Known                    bool       `json:"known"`
	RemainingPermille        int        `json:"remaining_permille"`
	ReservePermille          int        `json:"reserve_permille"`
	ReservedInflightPermille int        `json:"reserved_inflight_permille"`
	ObservedAt               *time.Time `json:"observed_at,omitempty"`
}

// admitAdaptiveTask resolves the INSERT-time task fence. It never changes the
// source agent identity or authority: active mode only selects an explicitly
// declared same-owner runtime/model configuration for that one task.
func (s *TaskService) admitAdaptiveTask(ctx context.Context, queued db.AgentTaskQueue) (db.AgentTaskQueue, error) {
	if queued.RouteAdmissionState != "pending" {
		return queued, nil
	}
	if s == nil || s.Queries == nil || s.TxStarter == nil {
		return queued, errors.New("adaptive admission requires transactional task service")
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return queued, fmt.Errorf("begin adaptive admission: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	task, err := qtx.LockTaskForAdaptiveAdmission(ctx, queued.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		// A duplicate notification may race the first admission. Return the
		// committed row so callers wake the selected runtime, not the stale one.
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			return queued, fmt.Errorf("rollback duplicate adaptive admission: %w", err)
		}
		current, getErr := s.Queries.GetAgentTask(ctx, queued.ID)
		if getErr != nil {
			return queued, getErr
		}
		return current, nil
	}
	if err != nil {
		return queued, fmt.Errorf("lock adaptive admission task: %w", err)
	}

	now := time.Now().UTC()
	mode := featureflags.AdaptiveAgentRoutingMode(ctx, s.FeatureFlags)
	commit := func(updated db.AgentTaskQueue) (db.AgentTaskQueue, error) {
		if err := tx.Commit(ctx); err != nil {
			return queued, fmt.Errorf("commit adaptive admission: %w", err)
		}
		return updated, nil
	}
	resolve := func(state string, record adaptiveAdmissionRecord) (db.AgentTaskQueue, error) {
		raw, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			return queued, fmt.Errorf("marshal adaptive admission decision: %w", marshalErr)
		}
		updated, updateErr := qtx.ResolveTaskAdaptiveAdmission(ctx, db.ResolveTaskAdaptiveAdmissionParams{
			RouteAdmissionState: state,
			RouteDecision:       raw,
			ID:                  task.ID,
		})
		if updateErr != nil {
			return queued, fmt.Errorf("resolve adaptive admission: %w", updateErr)
		}
		return commit(updated)
	}
	deferTask := func(record adaptiveAdmissionRecord) (db.AgentTaskQueue, error) {
		retryAt := now.Add(adaptiveAdmissionRetry)
		record.RetryAt = &retryAt
		raw, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			return queued, fmt.Errorf("marshal deferred adaptive admission: %w", marshalErr)
		}
		updated, updateErr := qtx.DeferTaskAdaptiveAdmission(ctx, db.DeferTaskAdaptiveAdmissionParams{
			RetryAfterSecs: adaptiveAdmissionRetry.Seconds(),
			RouteDecision:  raw,
			ID:             task.ID,
		})
		if updateErr != nil {
			return queued, fmt.Errorf("defer adaptive admission: %w", updateErr)
		}
		return commit(updated)
	}

	baseRecord := adaptiveAdmissionRecord{
		SchemaVersion: adaptiveRoutingDecisionVersion,
		Mode:          mode,
		EvaluatedAt:   now,
	}
	if mode == agentroute.ModeOff {
		baseRecord.Outcome = "original_route"
		baseRecord.Reason = "feature_disabled"
		return resolve("not_applicable", baseRecord)
	}

	agent, err := qtx.GetAgent(ctx, task.AgentID)
	if err != nil {
		return queued, fmt.Errorf("load adaptive admission agent: %w", err)
	}
	var envelope adaptiveRoutingEnvelope
	if err := json.Unmarshal(agent.RuntimeConfig, &envelope); err != nil {
		baseRecord.Outcome = "configuration_invalid"
		baseRecord.Reason = "runtime_config_parse_failed"
		if mode == agentroute.ModeShadow {
			return resolve("shadow", baseRecord)
		}
		return deferTask(baseRecord)
	}
	config := envelope.AdaptiveRouting
	if !config.Enabled {
		baseRecord.Outcome = "original_route"
		baseRecord.Reason = "agent_opt_out"
		return resolve("not_applicable", baseRecord)
	}
	if envelope.ProviderFailoverProtected || config.Protected {
		// Protected identities retain their fixed, independently approved
		// binding. They are excluded from automatic admission even if a stale
		// candidate list remains in runtime_config.
		baseRecord.Outcome = "original_route"
		baseRecord.Reason = "protected_identity"
		return resolve("not_applicable", baseRecord)
	}
	if !agent.OwnerID.Valid {
		baseRecord.Outcome = "configuration_invalid"
		baseRecord.Reason = "source_owner_missing"
		if mode == agentroute.ModeShadow {
			return resolve("shadow", baseRecord)
		}
		return deferTask(baseRecord)
	}
	if len(config.Candidates) == 0 {
		baseRecord.Outcome = "configuration_invalid"
		baseRecord.Reason = "candidate_set_empty"
		if mode == agentroute.ModeShadow {
			return resolve("shadow", baseRecord)
		}
		return deferTask(baseRecord)
	}

	risk, riskErr := normalizeAdaptiveRisk(config.Risk)
	if riskErr != nil {
		baseRecord.Outcome = "configuration_invalid"
		baseRecord.Reason = riskErr.Error()
		if mode == agentroute.ModeShadow {
			return resolve("shadow", baseRecord)
		}
		return deferTask(baseRecord)
	}

	capacityRows, err := qtx.ListProviderPlanCapacitiesForOwnerForUpdate(ctx, agent.OwnerID)
	if err != nil {
		return queued, fmt.Errorf("lock provider capacities: %w", err)
	}
	capacities, capacityRecords := adaptiveCapacities(now, capacityRows)
	baseRecord.Capacities = capacityRecords

	candidates, resolved, validationRejections := resolveAdaptiveCandidates(
		ctx,
		qtx,
		now,
		agent,
		config.Candidates,
	)
	baseRecord.ValidationRejections = validationRejections
	workload := agentroute.Workload{
		ID:                util.UUIDToString(task.ID),
		Risk:              risk,
		Urgency:           agentroute.UrgencyNormal,
		RequiredSkills:    config.RequiredSkills,
		RequiredTools:     config.RequiredTools,
		RequiredAuthority: config.RequiredAuthority,
	}
	if task.Priority >= 4 {
		workload.Urgency = agentroute.UrgencyEmergency
	}

	decision, routeErr := agentroute.Route(agentroute.Request{
		Workload:     workload,
		Candidates:   candidates,
		Capacities:   capacities,
		Affinities:   config.Affinities,
		MaxFallbacks: 2,
	})
	baseRecord.Topology = decision.Topology
	baseRecord.Rejections = adaptivePolicyRejections(decision.Rejections)
	baseRecord.Fallbacks = adaptiveFallbackRecords(decision.Fallbacks, resolved)
	if decision.Primary.Candidate.ID != "" {
		selected := adaptiveSelected(decision.Primary, resolved)
		baseRecord.Selected = &selected
	}

	if routeErr != nil {
		baseRecord.Outcome = "no_eligible_candidate"
		baseRecord.Reason = routeErr.Error()
		if mode == agentroute.ModeShadow {
			return resolve("shadow", baseRecord)
		}
		return deferTask(baseRecord)
	}
	selectedResolved, ok := resolved[decision.Primary.Candidate.ID]
	if !ok {
		return queued, fmt.Errorf("adaptive policy selected unresolved candidate %q", decision.Primary.Candidate.ID)
	}
	if mode == agentroute.ModeShadow {
		baseRecord.Outcome = "would_route"
		baseRecord.Reason = "shadow_only"
		return resolve("shadow", baseRecord)
	}

	forecast := int32(decision.Primary.Candidate.ExpectedUsePermille)
	if _, err := qtx.ReserveProviderPlanCapacity(ctx, db.ReserveProviderPlanCapacityParams{
		ReservePermille: forecast,
		OwnerID:         agent.OwnerID,
		Provider:        decision.Primary.Candidate.Provider,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			baseRecord.Outcome = "reservation_declined"
			baseRecord.Reason = "capacity_changed_before_reservation"
			return deferTask(baseRecord)
		}
		return queued, fmt.Errorf("reserve provider capacity: %w", err)
	}

	runtimeConfig := []byte(nil)
	if len(selectedResolved.Config.RuntimeConfig) > 0 {
		runtimeConfig = selectedResolved.Config.RuntimeConfig
	}
	customArgs := []byte(nil)
	if selectedResolved.Config.CustomArgs != nil {
		customArgs, err = json.Marshal(selectedResolved.Config.CustomArgs)
		if err != nil {
			return queued, fmt.Errorf("marshal adaptive custom args: %w", err)
		}
	}
	baseRecord.Outcome = "routed"
	baseRecord.Reason = "eligible_best_fit"
	rawDecision, err := json.Marshal(baseRecord)
	if err != nil {
		return queued, fmt.Errorf("marshal routed adaptive admission: %w", err)
	}
	updated, err := qtx.RouteTaskAdaptiveAdmission(ctx, db.RouteTaskAdaptiveAdmissionParams{
		RuntimeID:             selectedResolved.Runtime.ID,
		RouteDecision:         rawDecision,
		RouteProvider:         optionalText(decision.Primary.Candidate.Provider),
		RouteModel:            optionalText(selectedResolved.Config.Model),
		RouteThinkingLevel:    optionalText(selectedResolved.Config.ThinkingLevel),
		RouteServiceTier:      optionalText(selectedResolved.Config.ServiceTier),
		RouteRuntimeConfig:    runtimeConfig,
		RouteCustomArgs:       customArgs,
		RouteCapacityOwnerID:  agent.OwnerID,
		RouteReservedPermille: forecast,
		ID:                    task.ID,
	})
	if err != nil {
		return queued, fmt.Errorf("persist adaptive route: %w", err)
	}
	return commit(updated)
}

func normalizeAdaptiveRisk(value agentroute.Risk) (agentroute.Risk, error) {
	switch value {
	case "", agentroute.RiskMedium:
		return agentroute.RiskMedium, nil
	case agentroute.RiskLow, agentroute.RiskHigh, agentroute.RiskCritical:
		return value, nil
	default:
		return "", fmt.Errorf("invalid_risk_%s", value)
	}
}

func adaptiveCapacities(now time.Time, rows []db.ProviderPlanCapacity) ([]agentroute.Capacity, []adaptiveCapacityRecord) {
	capacities := make([]agentroute.Capacity, 0, len(rows))
	records := make([]adaptiveCapacityRecord, 0, len(rows))
	for _, row := range rows {
		remaining := int(row.RemainingPermille - row.ReservedInflightPermille)
		if remaining < 0 {
			remaining = 0
		}
		known := row.Known && row.ObservedAt.Valid
		if known {
			age := now.Sub(row.ObservedAt.Time)
			known = age >= -adaptiveFutureSkew && age <= adaptiveCapacityMaxAge
		}
		capacities = append(capacities, agentroute.Capacity{
			Provider:          row.Provider,
			Known:             known,
			RemainingPermille: remaining,
			ReservePermille:   int(row.ReservePermille),
		})
		record := adaptiveCapacityRecord{
			Provider:                 row.Provider,
			Known:                    known,
			RemainingPermille:        remaining,
			ReservePermille:          int(row.ReservePermille),
			ReservedInflightPermille: int(row.ReservedInflightPermille),
		}
		if row.ObservedAt.Valid {
			observed := row.ObservedAt.Time.UTC()
			record.ObservedAt = &observed
		}
		records = append(records, record)
	}
	return capacities, records
}

func resolveAdaptiveCandidates(
	ctx context.Context,
	q *db.Queries,
	now time.Time,
	agent db.Agent,
	configs []adaptiveCandidateConfig,
) ([]agentroute.Candidate, map[string]adaptiveResolvedCandidate, []adaptiveRejectionRecord) {
	candidates := make([]agentroute.Candidate, 0, len(configs))
	resolved := make(map[string]adaptiveResolvedCandidate, len(configs))
	rejections := make([]adaptiveRejectionRecord, 0)
	seen := make(map[string]struct{}, len(configs))
	for _, config := range configs {
		config.ID = strings.TrimSpace(config.ID)
		config.RuntimeID = strings.TrimSpace(config.RuntimeID)
		if config.ID == "" {
			rejections = append(rejections, adaptiveRejectionRecord{
				Reason: "candidate_id_missing",
			})
			continue
		}
		if _, exists := seen[config.ID]; exists {
			rejections = append(rejections, adaptiveRejectionRecord{
				CandidateID: config.ID,
				Reason:      "candidate_id_duplicate",
			})
			continue
		}
		seen[config.ID] = struct{}{}
		runtimeID, err := util.ParseUUID(config.RuntimeID)
		if err != nil {
			rejections = append(rejections, adaptiveRejectionRecord{
				CandidateID: config.ID,
				Reason:      "runtime_id_invalid",
			})
			continue
		}
		runtime, err := q.GetAgentRuntime(ctx, runtimeID)
		if err != nil {
			rejections = append(rejections, adaptiveRejectionRecord{
				CandidateID: config.ID,
				Reason:      "runtime_not_found",
			})
			continue
		}
		if util.UUIDToString(runtime.WorkspaceID) != util.UUIDToString(agent.WorkspaceID) {
			rejections = append(rejections, adaptiveRejectionRecord{
				CandidateID: config.ID,
				Reason:      "runtime_workspace_mismatch",
			})
			continue
		}
		if !runtime.OwnerID.Valid ||
			util.UUIDToString(runtime.OwnerID) != util.UUIDToString(agent.OwnerID) {
			rejections = append(rejections, adaptiveRejectionRecord{
				CandidateID: config.ID,
				Reason:      "runtime_owner_mismatch",
			})
			continue
		}
		online := runtime.Status == "online" && runtime.LastSeenAt.Valid
		if online {
			age := now.Sub(runtime.LastSeenAt.Time)
			online = age >= -adaptiveFutureSkew && age <= adaptiveRuntimeMaxAge
		}
		candidate := agentroute.Candidate{
			ID:                  config.ID,
			Provider:            strings.TrimSpace(runtime.Provider),
			Model:               strings.TrimSpace(config.Model),
			Thinking:            strings.TrimSpace(config.ThinkingLevel),
			Online:              online,
			SupportedSkills:     config.SupportedSkills,
			SupportedTools:      config.SupportedTools,
			AuthorityScopes:     config.AuthorityScopes,
			QualityBP:           config.QualityBP,
			LatencyPenaltyBP:    config.LatencyPenaltyBP,
			ExpectedUsePermille: config.ExpectedUsePermille,
		}
		candidates = append(candidates, candidate)
		resolved[config.ID] = adaptiveResolvedCandidate{Config: config, Runtime: runtime}
	}
	return candidates, resolved, rejections
}

func adaptivePolicyRejections(rejections []agentroute.Rejection) []adaptiveRejectionRecord {
	out := make([]adaptiveRejectionRecord, 0, len(rejections))
	for _, rejection := range rejections {
		out = append(out, adaptiveRejectionRecord{
			CandidateID: rejection.CandidateID,
			Reason:      string(rejection.Reason),
			Detail:      rejection.Detail,
		})
	}
	return out
}

func adaptiveFallbackRecords(
	fallbacks []agentroute.RankedCandidate,
	resolved map[string]adaptiveResolvedCandidate,
) []adaptiveSelectedRecord {
	out := make([]adaptiveSelectedRecord, 0, len(fallbacks))
	for _, fallback := range fallbacks {
		out = append(out, adaptiveSelected(fallback, resolved))
	}
	return out
}

func adaptiveSelected(
	ranked agentroute.RankedCandidate,
	resolved map[string]adaptiveResolvedCandidate,
) adaptiveSelectedRecord {
	runtimeID := ""
	if candidate, ok := resolved[ranked.Candidate.ID]; ok {
		runtimeID = util.UUIDToString(candidate.Runtime.ID)
	}
	return adaptiveSelectedRecord{
		CandidateID:                ranked.Candidate.ID,
		Provider:                   ranked.Candidate.Provider,
		RuntimeID:                  runtimeID,
		Model:                      ranked.Candidate.Model,
		ThinkingLevel:              ranked.Candidate.Thinking,
		ExpectedUsePermille:        ranked.Candidate.ExpectedUsePermille,
		ProjectedRemainingPermille: ranked.ProjectedRemainingPermille,
		ProjectedHeadroomPermille:  ranked.ProjectedHeadroomPermille,
		Score:                      ranked.Score,
	}
}

func optionalText(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	return pgtype.Text{String: value, Valid: value != ""}
}

// SweepPendingAdaptiveAdmissions recovers the narrow crash window between the
// INSERT trigger fencing a task and NotifyTaskEnqueued resolving that fence.
func (s *TaskService) SweepPendingAdaptiveAdmissions(ctx context.Context) {
	if s == nil || s.Queries == nil {
		return
	}
	promoted, err := s.Queries.PromoteDueAdaptiveRoutingTasks(ctx, adaptivePendingSweepBatch)
	if err != nil {
		slog.Warn("adaptive routing: promote due admissions failed", "error", err)
	} else {
		for _, row := range promoted {
			admitted, admitErr := s.admitAdaptiveTask(ctx, row)
			if admitErr != nil {
				slog.Warn("adaptive routing: re-admit deferred task failed",
					"task_id", util.UUIDToString(row.ID),
					"error", admitErr,
				)
				continue
			}
			if admitted.Status == "queued" && admitted.RouteAdmissionState != "pending" {
				s.notifyTaskAvailable(admitted)
			}
		}
	}
	rows, err := s.Queries.ListPendingAdaptiveAdmissions(ctx, db.ListPendingAdaptiveAdmissionsParams{
		MinAgeSecs: adaptivePendingSweepMinAge.Seconds(),
		MaxRows:    adaptivePendingSweepBatch,
	})
	if err != nil {
		slog.Warn("adaptive routing: list pending admissions failed", "error", err)
		return
	}
	for _, row := range rows {
		admitted, err := s.admitAdaptiveTask(ctx, row)
		if err != nil {
			slog.Warn("adaptive routing: recover pending admission failed",
				"task_id", util.UUIDToString(row.ID),
				"error", err,
			)
			continue
		}
		if admitted.Status == "queued" && admitted.RouteAdmissionState != "pending" {
			s.notifyTaskAvailable(admitted)
		}
	}
}
