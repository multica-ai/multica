package aiimpact

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
)

var ErrReadOnly = errors.New("AI Impact is read-only for workspace members")

type ServiceStore interface {
	CreateFunction(ctx context.Context, workspaceID uuid.UUID, input FunctionInput) (Function, error)
	ListFunctions(ctx context.Context, workspaceID uuid.UUID) ([]Function, error)
	CreateOperatingLoop(ctx context.Context, workspaceID uuid.UUID, input OperatingLoopInput) (OperatingLoop, error)
	ListOperatingLoops(ctx context.Context, workspaceID uuid.UUID) ([]OperatingLoop, error)
	CreateProjectBinding(ctx context.Context, workspaceID uuid.UUID, input ProjectBindingInput) (ProjectBinding, error)
	ListProjectBindings(ctx context.Context, workspaceID uuid.UUID) ([]ProjectBinding, error)
	CreateMetric(ctx context.Context, workspaceID uuid.UUID, input MetricInput) (Metric, error)
	ListMetrics(ctx context.Context, workspaceID uuid.UUID) ([]Metric, error)
	AppendObservation(
		ctx context.Context,
		workspaceID, actorID uuid.UUID,
		actorType string,
		input ObservationInput,
	) (Observation, error)
	ListObservations(ctx context.Context, workspaceID, metricID uuid.UUID) ([]Observation, error)
	ListWorkspaceObservations(ctx context.Context, workspaceID uuid.UUID) ([]Observation, error)
}

func (s *Service) ListFunctions(ctx context.Context, workspaceID uuid.UUID) ([]Function, error) {
	return s.store.ListFunctions(ctx, workspaceID)
}

func (s *Service) ListOperatingLoops(ctx context.Context, workspaceID uuid.UUID) ([]OperatingLoop, error) {
	return s.store.ListOperatingLoops(ctx, workspaceID)
}

func (s *Service) ListProjectBindings(ctx context.Context, workspaceID uuid.UUID) ([]ProjectBinding, error) {
	return s.store.ListProjectBindings(ctx, workspaceID)
}

func (s *Service) ListMetrics(ctx context.Context, workspaceID uuid.UUID) ([]Metric, error) {
	return s.store.ListMetrics(ctx, workspaceID)
}

func (s *Service) CreateProjectBinding(
	ctx context.Context,
	workspaceID uuid.UUID,
	role string,
	input ProjectBindingInput,
) (ProjectBinding, error) {
	if !CanConfigure(role) {
		return ProjectBinding{}, ErrReadOnly
	}
	return s.store.CreateProjectBinding(ctx, workspaceID, input)
}

func (s *Service) CreateMetric(
	ctx context.Context,
	workspaceID uuid.UUID,
	role string,
	input MetricInput,
) (Metric, error) {
	if !CanConfigure(role) {
		return Metric{}, ErrReadOnly
	}
	return s.store.CreateMetric(ctx, workspaceID, input)
}

type Service struct {
	store ServiceStore
}

func NewService(store ServiceStore) *Service {
	return &Service{store: store}
}

func (s *Service) CreateFunction(
	ctx context.Context,
	workspaceID uuid.UUID,
	role string,
	input FunctionInput,
) (Function, error) {
	if !CanConfigure(role) {
		return Function{}, ErrReadOnly
	}
	return s.store.CreateFunction(ctx, workspaceID, input)
}

func (s *Service) CreateOperatingLoop(
	ctx context.Context,
	workspaceID uuid.UUID,
	role string,
	input OperatingLoopInput,
) (OperatingLoop, error) {
	if !CanConfigure(role) {
		return OperatingLoop{}, ErrReadOnly
	}
	return s.store.CreateOperatingLoop(ctx, workspaceID, input)
}

func (s *Service) AppendObservation(
	ctx context.Context,
	workspaceID, actorID uuid.UUID,
	actorType, role string,
	input ObservationInput,
) (Observation, error) {
	if !CanConfigure(role) {
		return Observation{}, ErrReadOnly
	}
	return s.store.AppendObservation(ctx, workspaceID, actorID, actorType, input)
}

func (s *Service) ListObservations(
	ctx context.Context,
	workspaceID, metricID uuid.UUID,
) ([]Observation, error) {
	return s.store.ListObservations(ctx, workspaceID, metricID)
}

func (s *Service) ListWorkspaceLatestObservations(
	ctx context.Context,
	workspaceID uuid.UUID,
) ([]Observation, error) {
	observations, err := s.store.ListWorkspaceObservations(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return LatestObservations(observations), nil
}

// ListWorkspaceEvidence returns each latest observation with its Function, Operating Loop, and Metric.
func (s *Service) ListWorkspaceEvidence(
	ctx context.Context,
	workspaceID uuid.UUID,
) ([]EvidenceReadModel, error) {
	return s.listWorkspaceEvidence(ctx, workspaceID, EvidenceFilter{})
}

func (s *Service) listWorkspaceEvidence(
	ctx context.Context,
	workspaceID uuid.UUID,
	filter EvidenceFilter,
) ([]EvidenceReadModel, error) {
	functions, err := s.store.ListFunctions(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	operatingLoops, err := s.store.ListOperatingLoops(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	metrics, err := s.store.ListMetrics(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	observations, err := s.store.ListWorkspaceObservations(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	functionsByID := make(map[uuid.UUID]Function, len(functions))
	for _, function := range functions {
		functionsByID[function.ID] = function
	}
	loopsByID := make(map[uuid.UUID]OperatingLoop, len(operatingLoops))
	for _, operatingLoop := range operatingLoops {
		loopsByID[operatingLoop.ID] = operatingLoop
	}
	metricsByID := make(map[uuid.UUID]Metric, len(metrics))
	for _, metric := range metrics {
		metricsByID[metric.ID] = metric
	}

	evidence := make([]EvidenceReadModel, 0, len(observations))
	filteredObservations := make([]Observation, 0, len(observations))
	for _, observation := range observations {
		if observation.Confidence < filter.MinimumConfidence {
			continue
		}
		if filter.Source != "" && observation.Source != filter.Source {
			continue
		}
		if !filter.PeriodStart.IsZero() && observation.PeriodStart.Before(filter.PeriodStart) {
			continue
		}
		if !filter.PeriodEnd.IsZero() && observation.PeriodEnd.After(filter.PeriodEnd) {
			continue
		}
		filteredObservations = append(filteredObservations, observation)
	}

	for _, observation := range LatestObservations(filteredObservations) {
		metric, ok := metricsByID[observation.MetricID]
		if !ok {
			continue
		}
		operatingLoop, ok := loopsByID[metric.OperatingLoopID]
		if !ok {
			continue
		}
		function, ok := functionsByID[operatingLoop.FunctionID]
		if !ok {
			continue
		}
		evidence = append(evidence, EvidenceReadModel{
			Function:      function,
			OperatingLoop: operatingLoop,
			Metric:        metric,
			Observation:   observation,
		})
	}
	return evidence, nil
}

type EvidenceFilter struct {
	FunctionID        uuid.UUID
	OperatingLoopID   uuid.UUID
	MetricID          uuid.UUID
	Guardrail         *bool
	MetricFamily      MetricFamily
	EvidenceStatus    EvidenceStatus
	Source            string
	MinimumConfidence float64
	PeriodStart       time.Time
	PeriodEnd         time.Time
}

// QualityRiskDecisionReadModel gives one evidence-based decision per Operating Loop.
type QualityRiskDecisionReadModel struct {
	Function      Function
	OperatingLoop OperatingLoop
	Decision      Decision
}

type FunctionSummaryLoopReadModel struct {
	OperatingLoop OperatingLoop
	Decision      Decision
}

// FunctionSummaryReadModel groups active Operating Loops and their decisions by Function.
type FunctionSummaryReadModel struct {
	Function       Function
	OperatingLoops []FunctionSummaryLoopReadModel
}

// ListQualityRiskDecisions evaluates the latest targeted Outcome and guardrail evidence.
func (s *Service) ListQualityRiskDecisions(
	ctx context.Context,
	workspaceID uuid.UUID,
) ([]QualityRiskDecisionReadModel, error) {
	functions, err := s.store.ListFunctions(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	operatingLoops, err := s.store.ListOperatingLoops(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	evidence, err := s.ListWorkspaceEvidence(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	type decisionEvidence struct {
		function            Function
		operatingLoop       OperatingLoop
		hasOutcome          bool
		outcomesPositive    bool
		hasDecisionEvidence bool
		evidenceMeasured    bool
		guardrails          []GuardrailResult
	}
	byLoop := make(map[uuid.UUID]*decisionEvidence)
	loopOrder := make([]uuid.UUID, 0, len(operatingLoops))
	functionsByID := make(map[uuid.UUID]Function, len(functions))
	for _, function := range functions {
		functionsByID[function.ID] = function
	}
	for _, operatingLoop := range operatingLoops {
		function, ok := functionsByID[operatingLoop.FunctionID]
		if !ok || !function.Active || !operatingLoop.Active {
			continue
		}
		byLoop[operatingLoop.ID] = &decisionEvidence{
			function:         function,
			operatingLoop:    operatingLoop,
			outcomesPositive: true,
			evidenceMeasured: true,
		}
		loopOrder = append(loopOrder, operatingLoop.ID)
	}
	for _, item := range evidence {
		if item.Metric.TargetValue == nil || (!item.Metric.Guardrail && item.Metric.Family != FamilyOutcome) {
			continue
		}
		state, ok := byLoop[item.OperatingLoop.ID]
		if !ok {
			continue
		}

		passed := metricMeetsTarget(item.Metric, item.Observation.Value)
		state.hasDecisionEvidence = true
		if item.Observation.EvidenceStatus != EvidenceMeasured {
			state.evidenceMeasured = false
		}
		if item.Metric.Family == FamilyOutcome {
			state.hasOutcome = true
			state.outcomesPositive = state.outcomesPositive && passed
		}
		if item.Metric.Guardrail {
			state.guardrails = append(state.guardrails, GuardrailResult{Critical: true, Passed: passed})
		}
	}

	result := make([]QualityRiskDecisionReadModel, 0, len(loopOrder))
	for _, loopID := range loopOrder {
		state := byLoop[loopID]
		decision := ComputeDecision(DecisionInput{
			OutcomePositive:  state.hasOutcome && state.outcomesPositive,
			EvidenceMeasured: state.hasDecisionEvidence && state.evidenceMeasured,
			Guardrails:       state.guardrails,
		}).Decision
		result = append(result, QualityRiskDecisionReadModel{
			Function:      state.function,
			OperatingLoop: state.operatingLoop,
			Decision:      decision,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].OperatingLoop.Name == result[j].OperatingLoop.Name {
			return result[i].OperatingLoop.ID.String() < result[j].OperatingLoop.ID.String()
		}
		return result[i].OperatingLoop.Name < result[j].OperatingLoop.Name
	})
	return result, nil
}

// ListFunctionSummaries returns active Functions with their active Operating Loop decisions.
func (s *Service) ListFunctionSummaries(
	ctx context.Context,
	workspaceID uuid.UUID,
) ([]FunctionSummaryReadModel, error) {
	functions, err := s.store.ListFunctions(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	decisions, err := s.ListQualityRiskDecisions(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	byFunction := make(map[uuid.UUID][]FunctionSummaryLoopReadModel, len(functions))
	for _, item := range decisions {
		byFunction[item.Function.ID] = append(byFunction[item.Function.ID], FunctionSummaryLoopReadModel{
			OperatingLoop: item.OperatingLoop,
			Decision:      item.Decision,
		})
	}

	result := make([]FunctionSummaryReadModel, 0, len(functions))
	for _, function := range functions {
		if !function.Active {
			continue
		}
		loops := byFunction[function.ID]
		if loops == nil {
			loops = []FunctionSummaryLoopReadModel{}
		}
		result = append(result, FunctionSummaryReadModel{
			Function:       function,
			OperatingLoops: loops,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Function.Name == result[j].Function.Name {
			return result[i].Function.ID.String() < result[j].Function.ID.String()
		}
		return result[i].Function.Name < result[j].Function.Name
	})
	return result, nil
}

// ListFilteredEvidence returns latest workspace evidence matching every supplied filter.
func (s *Service) ListFilteredEvidence(
	ctx context.Context,
	workspaceID uuid.UUID,
	filter EvidenceFilter,
) ([]EvidenceReadModel, error) {
	evidence, err := s.listWorkspaceEvidence(ctx, workspaceID, filter)
	if err != nil {
		return nil, err
	}

	result := make([]EvidenceReadModel, 0, len(evidence))
	for _, item := range evidence {
		if filter.FunctionID != uuid.Nil && item.Function.ID != filter.FunctionID {
			continue
		}
		if filter.OperatingLoopID != uuid.Nil && item.OperatingLoop.ID != filter.OperatingLoopID {
			continue
		}
		if filter.MetricID != uuid.Nil && item.Metric.ID != filter.MetricID {
			continue
		}
		if filter.Guardrail != nil && item.Metric.Guardrail != *filter.Guardrail {
			continue
		}
		if filter.MetricFamily != "" && item.Metric.Family != filter.MetricFamily {
			continue
		}
		if filter.EvidenceStatus != "" && item.Observation.EvidenceStatus != filter.EvidenceStatus {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

// ListMetricFamilyEvidence returns the latest workspace evidence for one metric family.
func (s *Service) ListMetricFamilyEvidence(
	ctx context.Context,
	workspaceID uuid.UUID,
	family MetricFamily,
) ([]EvidenceReadModel, error) {
	return s.ListFilteredEvidence(ctx, workspaceID, EvidenceFilter{MetricFamily: family})
}

// ListEvidenceStatusEvidence returns the latest workspace evidence for one evidence status.
func (s *Service) ListEvidenceStatusEvidence(
	ctx context.Context,
	workspaceID uuid.UUID,
	status EvidenceStatus,
) ([]EvidenceReadModel, error) {
	return s.ListFilteredEvidence(ctx, workspaceID, EvidenceFilter{EvidenceStatus: status})
}

// ListFunctionEvidence returns the latest workspace evidence for one Function.
func (s *Service) ListFunctionEvidence(
	ctx context.Context,
	workspaceID, functionID uuid.UUID,
) ([]EvidenceReadModel, error) {
	evidence, err := s.ListWorkspaceEvidence(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	result := make([]EvidenceReadModel, 0, len(evidence))
	for _, item := range evidence {
		if item.Function.ID == functionID {
			result = append(result, item)
		}
	}
	return result, nil
}

// ListOperatingLoopEvidence returns the latest workspace evidence for one Operating Loop.
func (s *Service) ListOperatingLoopEvidence(
	ctx context.Context,
	workspaceID, operatingLoopID uuid.UUID,
) ([]EvidenceReadModel, error) {
	evidence, err := s.ListWorkspaceEvidence(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	result := make([]EvidenceReadModel, 0, len(evidence))
	for _, item := range evidence {
		if item.OperatingLoop.ID == operatingLoopID {
			result = append(result, item)
		}
	}
	return result, nil
}

// ListMetricEvidence returns the latest workspace evidence for one Metric.
func (s *Service) ListMetricEvidence(
	ctx context.Context,
	workspaceID, metricID uuid.UUID,
) ([]EvidenceReadModel, error) {
	evidence, err := s.ListWorkspaceEvidence(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	result := make([]EvidenceReadModel, 0, len(evidence))
	for _, item := range evidence {
		if item.Metric.ID == metricID {
			result = append(result, item)
		}
	}
	return result, nil
}
