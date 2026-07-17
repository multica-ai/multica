package aiimpact

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingObservationStore struct {
	appended int
}

func (s *recordingObservationStore) AppendObservation(
	_ context.Context,
	_, _ uuid.UUID,
	_ string,
	input ObservationInput,
) (Observation, error) {
	s.appended++
	return Observation{ID: uuid.New(), MetricID: input.MetricID, Value: input.Value}, nil
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
	if store.appended != 2 {
		t.Fatalf("store append count = %d, want only owner and admin writes", store.appended)
	}
}
