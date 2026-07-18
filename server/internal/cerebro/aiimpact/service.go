package aiimpact

import (
	"context"
	"errors"

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
