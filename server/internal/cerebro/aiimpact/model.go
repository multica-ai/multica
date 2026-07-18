package aiimpact

import (
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
)

type MetricFamily string

const (
	FamilyAdoption  MetricFamily = "Adoption"
	FamilyOutput    MetricFamily = "Output"
	FamilyOutcome   MetricFamily = "Outcome"
	FamilyQuality   MetricFamily = "Quality"
	FamilyEconomics MetricFamily = "Economics"
	FamilyRisk      MetricFamily = "Risk"
)

type MetricDirection string

const (
	DirectionIncrease MetricDirection = "increase"
	DirectionDecrease MetricDirection = "decrease"
)

type EvidenceStatus string

const (
	EvidenceMeasured  EvidenceStatus = "Measured"
	EvidenceEstimated EvidenceStatus = "Estimated"
	EvidenceMissing   EvidenceStatus = "Missing"
)

type Decision string

const (
	DecisionScale   Decision = "Scale"
	DecisionObserve Decision = "Observe"
	DecisionStop    Decision = "Stop"
)

type FunctionInput struct {
	Name        string
	Description string
	OwnerType   string
	OwnerID     uuid.UUID
}

func ValidateFunction(input FunctionInput) error {
	if input.Name == "" {
		return errors.New("function name is required")
	}
	if input.OwnerType != "member" && input.OwnerType != "agent" {
		return errors.New("function owner type must be member or agent")
	}
	if input.OwnerID == uuid.Nil {
		return errors.New("function owner id is required")
	}
	return nil
}

type Function struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Name        string
	Description string
	OwnerType   string
	OwnerID     uuid.UUID
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type OperatingLoopInput struct {
	FunctionID  uuid.UUID
	Name        string
	Description string
}

func ValidateOperatingLoop(input OperatingLoopInput) error {
	if input.FunctionID == uuid.Nil {
		return errors.New("operating loop function id is required")
	}
	if input.Name == "" {
		return errors.New("operating loop name is required")
	}
	return nil
}

type OperatingLoop struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	FunctionID  uuid.UUID
	Name        string
	Description string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProjectBindingInput struct {
	ProjectID       uuid.UUID
	OperatingLoopID uuid.UUID
}

func ValidateProjectBinding(input ProjectBindingInput) error {
	if input.ProjectID == uuid.Nil {
		return errors.New("project binding project id is required")
	}
	if input.OperatingLoopID == uuid.Nil {
		return errors.New("project binding operating loop id is required")
	}
	return nil
}

type ProjectBinding struct {
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	ProjectID       uuid.UUID
	OperatingLoopID uuid.UUID
	Active          bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type MetricInput struct {
	OperatingLoopID uuid.UUID
	Name            string
	Family          MetricFamily
	Unit            string
	Direction       MetricDirection
	BaselineStart   time.Time
	BaselineEnd     time.Time
	Source          string
	Guardrail       bool
}

func ValidateMetric(input MetricInput) error {
	if input.OperatingLoopID == uuid.Nil {
		return errors.New("metric operating loop id is required")
	}
	if input.Name == "" || input.Unit == "" || input.Source == "" {
		return errors.New("metric name, unit, and source are required")
	}
	if !validMetricFamily(input.Family) {
		return errors.New("invalid metric family")
	}
	if input.Direction != DirectionIncrease && input.Direction != DirectionDecrease {
		return errors.New("invalid metric direction")
	}
	if input.BaselineStart.IsZero() || input.BaselineEnd.IsZero() || !input.BaselineStart.Before(input.BaselineEnd) {
		return errors.New("metric baseline must have an ordered start and end")
	}
	return nil
}

type Metric struct {
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	OperatingLoopID uuid.UUID
	Name            string
	Family          MetricFamily
	Unit            string
	Direction       MetricDirection
	BaselineStart   time.Time
	BaselineEnd     time.Time
	Source          string
	Guardrail       bool
	Active          bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func validMetricFamily(family MetricFamily) bool {
	switch family {
	case FamilyAdoption, FamilyOutput, FamilyOutcome, FamilyQuality, FamilyEconomics, FamilyRisk:
		return true
	default:
		return false
	}
}

type ObservationInput struct {
	MetricID       uuid.UUID
	PeriodStart    time.Time
	PeriodEnd      time.Time
	Value          float64
	EvidenceStatus EvidenceStatus
	Confidence     float64
	Source         string
	Method         string
}

func ValidateObservation(input ObservationInput) error {
	if input.MetricID == uuid.Nil {
		return errors.New("metric id is required")
	}
	if input.PeriodStart.IsZero() || input.PeriodEnd.IsZero() || !input.PeriodStart.Before(input.PeriodEnd) {
		return errors.New("observation period must have an ordered start and end")
	}
	if !validEvidenceStatus(input.EvidenceStatus) {
		return errors.New("invalid evidence status")
	}
	if input.Confidence < 0 || input.Confidence > 1 {
		return errors.New("confidence must be between zero and one")
	}
	if input.Source == "" || input.Method == "" {
		return errors.New("observation source and method are required")
	}
	return nil
}

func validEvidenceStatus(status EvidenceStatus) bool {
	switch status {
	case EvidenceMeasured, EvidenceEstimated, EvidenceMissing:
		return true
	default:
		return false
	}
}

type Observation struct {
	ID             uuid.UUID
	MetricID       uuid.UUID
	PeriodStart    time.Time
	PeriodEnd      time.Time
	Value          float64
	EvidenceStatus EvidenceStatus
	Confidence     float64
	Source         string
	Method         string
	CreatedAt      time.Time
}

// EvidenceReadModel places one latest observation in its business taxonomy.
type EvidenceReadModel struct {
	Function      Function
	OperatingLoop OperatingLoop
	Metric        Metric
	Observation   Observation
}

type GuardrailResult struct {
	Critical bool
	Passed   bool
}

type DecisionInput struct {
	RealizedCashCents       int64
	ApprovedCapacityCents   int64
	EstimatedValueCents     int64
	AICostCents             int64
	ImplementationCostCents int64
	OutcomePositive         bool
	EvidenceMeasured        bool
	Guardrails              []GuardrailResult
}

type DecisionResult struct {
	NetValueCents       int64
	EstimatedValueCents int64
	Decision            Decision
}

func ComputeDecision(input DecisionInput) DecisionResult {
	result := DecisionResult{
		NetValueCents:       input.RealizedCashCents + input.ApprovedCapacityCents - input.AICostCents - input.ImplementationCostCents,
		EstimatedValueCents: input.EstimatedValueCents,
		Decision:            DecisionObserve,
	}

	allGuardrailsPassed := len(input.Guardrails) > 0
	for _, guardrail := range input.Guardrails {
		if guardrail.Critical && !guardrail.Passed {
			result.Decision = DecisionStop
			return result
		}
		if !guardrail.Passed {
			allGuardrailsPassed = false
		}
	}

	if allGuardrailsPassed && input.OutcomePositive && input.EvidenceMeasured {
		result.Decision = DecisionScale
	}
	return result
}

func LatestObservations(observations []Observation) []Observation {
	type periodKey struct {
		MetricID    uuid.UUID
		PeriodStart time.Time
		PeriodEnd   time.Time
	}

	latest := make(map[periodKey]Observation, len(observations))
	for _, observation := range observations {
		key := periodKey{
			MetricID:    observation.MetricID,
			PeriodStart: observation.PeriodStart,
			PeriodEnd:   observation.PeriodEnd,
		}
		current, ok := latest[key]
		if !ok || observation.CreatedAt.After(current.CreatedAt) {
			latest[key] = observation
		}
	}

	result := make([]Observation, 0, len(latest))
	for _, observation := range latest {
		result = append(result, observation)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].PeriodStart.Equal(result[j].PeriodStart) {
			if result[i].PeriodEnd.Equal(result[j].PeriodEnd) {
				return result[i].MetricID.String() < result[j].MetricID.String()
			}
			return result[i].PeriodEnd.Before(result[j].PeriodEnd)
		}
		return result[i].PeriodStart.Before(result[j].PeriodStart)
	})
	return result
}

func CanConfigure(role string) bool {
	return role == "owner" || role == "admin"
}
