package aiimpact

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingObservationStore struct {
	observations []Observation
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
