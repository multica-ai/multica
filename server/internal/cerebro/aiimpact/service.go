package aiimpact

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrReadOnly = errors.New("AI Impact is read-only for workspace members")

type ObservationStore interface {
	AppendObservation(
		ctx context.Context,
		workspaceID, actorID uuid.UUID,
		actorType string,
		input ObservationInput,
	) (Observation, error)
}

type Service struct {
	store ObservationStore
}

func NewService(store ObservationStore) *Service {
	return &Service{store: store}
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
