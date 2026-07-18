package aiimpact

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingObservationStore struct {
	observations             []Observation
	functions                []Function
	operatingLoops           []OperatingLoop
	listFunctionsWorkspaceID uuid.UUID
	listLoopsWorkspaceID     uuid.UUID
}

func (s *recordingObservationStore) ListFunctions(
	_ context.Context,
	workspaceID uuid.UUID,
) ([]Function, error) {
	s.listFunctionsWorkspaceID = workspaceID
	return append([]Function(nil), s.functions...), nil
}

func (s *recordingObservationStore) CreateFunction(
	_ context.Context,
	workspaceID uuid.UUID,
	input FunctionInput,
) (Function, error) {
	return Function{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Name:        input.Name,
		Description: input.Description,
		OwnerType:   input.OwnerType,
		OwnerID:     input.OwnerID,
		Active:      true,
	}, nil
}

func (s *recordingObservationStore) CreateOperatingLoop(
	_ context.Context,
	workspaceID uuid.UUID,
	input OperatingLoopInput,
) (OperatingLoop, error) {
	return OperatingLoop{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		FunctionID:  input.FunctionID,
		Name:        input.Name,
		Description: input.Description,
		Active:      true,
	}, nil
}

func (s *recordingObservationStore) ListOperatingLoops(
	_ context.Context,
	workspaceID uuid.UUID,
) ([]OperatingLoop, error) {
	s.listLoopsWorkspaceID = workspaceID
	return append([]OperatingLoop(nil), s.operatingLoops...), nil
}

func (s *recordingObservationStore) CreateMetric(
	_ context.Context,
	workspaceID uuid.UUID,
	input MetricInput,
) (Metric, error) {
	return Metric{
		ID:              uuid.New(),
		WorkspaceID:     workspaceID,
		OperatingLoopID: input.OperatingLoopID,
		Name:            input.Name,
		Family:          input.Family,
		Unit:            input.Unit,
		Direction:       input.Direction,
		BaselineStart:   input.BaselineStart,
		BaselineEnd:     input.BaselineEnd,
		Source:          input.Source,
		Guardrail:       input.Guardrail,
		Active:          true,
	}, nil
}

func (s *recordingObservationStore) CreateProjectBinding(
	_ context.Context,
	workspaceID uuid.UUID,
	input ProjectBindingInput,
) (ProjectBinding, error) {
	return ProjectBinding{
		ID:              uuid.New(),
		WorkspaceID:     workspaceID,
		ProjectID:       input.ProjectID,
		OperatingLoopID: input.OperatingLoopID,
		Active:          true,
	}, nil
}

func (s *recordingObservationStore) AppendObservation(
	_ context.Context,
	_, _ uuid.UUID,
	_ string,
	input ObservationInput,
) (Observation, error) {
	observation := Observation{ID: uuid.New(), MetricID: input.MetricID, Value: input.Value}
	s.observations = append(s.observations, observation)
	return observation, nil
}

func (s *recordingObservationStore) ListObservations(
	_ context.Context,
	_, _ uuid.UUID,
) ([]Observation, error) {
	return append([]Observation(nil), s.observations...), nil
}

func TestServiceAppendObservationAllowsOwnerAdminAndKeepsMemberReadOnly(t *testing.T) {
	store := &recordingObservationStore{}
	service := NewService(store)
	workspaceID := uuid.New()
	actorID := uuid.New()
	input := ObservationInput{
		MetricID:       uuid.New(),
		PeriodStart:    time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:      time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC),
		Value:          12,
		EvidenceStatus: EvidenceMeasured,
		Confidence:     0.9,
		Source:         "support",
		Method:         "audited count",
	}

	for _, role := range []string{"owner", "admin"} {
		observation, err := service.AppendObservation(
			context.Background(), workspaceID, actorID, "member", role, input,
		)
		if err != nil {
			t.Fatalf("%s append observation: %v", role, err)
		}
		if observation.ID == uuid.Nil {
			t.Fatalf("%s append returned no observation", role)
		}
	}

	if _, err := service.AppendObservation(
		context.Background(), workspaceID, actorID, "member", "member", input,
	); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("member append error = %v, want ErrReadOnly", err)
	}
	observations, err := service.ListObservations(context.Background(), workspaceID, input.MetricID)
	if err != nil {
		t.Fatalf("member list observations: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("member read count = %d, want the owner and admin observations", len(observations))
	}
}
