package cerebro

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// ObservationInput is the payload sent by the agent runtime (or a test) when
// it detects that a skill run produced delta behavior beyond the instructions.
type ObservationInput struct {
	SkillID      pgtype.UUID
	SkillVersion string
	RunID        pgtype.UUID  // nullable
	AutopilotID  pgtype.UUID  // nullable
	DeltaType    string       // extra_table_access | extra_verification_step | output_format_deviation | skill_chain_addition
	DeltaValue   string       // e.g. 'firtal_dwh.product_margin_daily'
	Confidence   float64
	Outcome      string       // 'success' | 'failure'
}

// RecordObservation persists a skill observation and updates the running aggregate.
func RecordObservation(ctx context.Context, q *cerebrodb.Queries, obs ObservationInput) error {
	if obs.Outcome == "" {
		obs.Outcome = "success"
	}
	if obs.Confidence <= 0 {
		obs.Confidence = 1.0
	}

	if _, err := q.InsertSkillObservation(ctx, cerebrodb.InsertSkillObservationParams{
		SkillID:      obs.SkillID,
		SkillVersion: obs.SkillVersion,
		RunID:        obs.RunID,
		AutopilotID:  obs.AutopilotID,
		DeltaType:    obs.DeltaType,
		DeltaValue:   obs.DeltaValue,
		Confidence:   obs.Confidence,
		Outcome:      obs.Outcome,
	}); err != nil {
		return err
	}

	return q.UpsertObservationAggregate(ctx, cerebrodb.UpsertObservationAggregateParams{
		SkillID:    obs.SkillID,
		DeltaType:  obs.DeltaType,
		DeltaValue: obs.DeltaValue,
		Outcome:    obs.Outcome,
	})
}
