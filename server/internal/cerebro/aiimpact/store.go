package aiimpact

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("AI Impact resource not found")

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const observationColumns = `id, metric_id, period_start, period_end, value,
 evidence_status, confidence, source, method, created_at`

func scanObservation(row pgx.Row) (Observation, error) {
	var observation Observation
	var value pgtype.Float8
	err := row.Scan(
		&observation.ID,
		&observation.MetricID,
		&observation.PeriodStart,
		&observation.PeriodEnd,
		&value,
		&observation.EvidenceStatus,
		&observation.Confidence,
		&observation.Source,
		&observation.Method,
		&observation.CreatedAt,
	)
	if value.Valid {
		observation.Value = value.Float64
	}
	return observation, err
}

func (s *Store) AppendObservation(
	ctx context.Context,
	workspaceID, actorID uuid.UUID,
	actorType string,
	input ObservationInput,
) (Observation, error) {
	if err := ValidateObservation(input); err != nil {
		return Observation{}, err
	}

	var value any = input.Value
	if input.EvidenceStatus == EvidenceMissing {
		value = nil
	}
	var createdByID any = actorID
	if actorType == "system" {
		createdByID = nil
	}

	observation, err := scanObservation(s.pool.QueryRow(ctx, `
		INSERT INTO cerebro_ai_impact_observation
			(workspace_id, metric_id, period_start, period_end, value,
			 evidence_status, confidence, source, method, created_by_type, created_by_id)
		SELECT $1, metric.id, $3, $4, $5, $6, $7, $8, $9, $10, $11
		FROM cerebro_ai_impact_metric metric
		WHERE metric.workspace_id = $1 AND metric.id = $2
		RETURNING `+observationColumns,
		workspaceID,
		input.MetricID,
		input.PeriodStart,
		input.PeriodEnd,
		value,
		input.EvidenceStatus,
		input.Confidence,
		input.Source,
		input.Method,
		actorType,
		createdByID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Observation{}, ErrNotFound
	}
	return observation, err
}

func (s *Store) ListObservations(ctx context.Context, workspaceID, metricID uuid.UUID) ([]Observation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+observationColumns+`
		FROM cerebro_ai_impact_observation
		WHERE workspace_id = $1 AND metric_id = $2
		ORDER BY created_at, id`, workspaceID, metricID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	observations := make([]Observation, 0)
	for rows.Next() {
		observation, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	return observations, rows.Err()
}
