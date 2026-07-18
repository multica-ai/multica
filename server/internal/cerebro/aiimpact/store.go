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

const functionColumns = `id, workspace_id, name, description, owner_type, owner_id,
 active, created_at, updated_at`

func scanFunction(row pgx.Row) (Function, error) {
	var function Function
	err := row.Scan(
		&function.ID,
		&function.WorkspaceID,
		&function.Name,
		&function.Description,
		&function.OwnerType,
		&function.OwnerID,
		&function.Active,
		&function.CreatedAt,
		&function.UpdatedAt,
	)
	return function, err
}

func (s *Store) CreateFunction(
	ctx context.Context,
	workspaceID uuid.UUID,
	input FunctionInput,
) (Function, error) {
	if err := ValidateFunction(input); err != nil {
		return Function{}, err
	}
	return scanFunction(s.pool.QueryRow(ctx, `
		INSERT INTO cerebro_ai_impact_function
			(workspace_id, name, description, owner_type, owner_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+functionColumns,
		workspaceID,
		input.Name,
		input.Description,
		input.OwnerType,
		input.OwnerID,
	))
}

func (s *Store) ListFunctions(ctx context.Context, workspaceID uuid.UUID) ([]Function, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+functionColumns+`
		FROM cerebro_ai_impact_function
		WHERE workspace_id = $1
		ORDER BY name, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	functions := make([]Function, 0)
	for rows.Next() {
		function, err := scanFunction(rows)
		if err != nil {
			return nil, err
		}
		functions = append(functions, function)
	}
	return functions, rows.Err()
}

const operatingLoopColumns = `id, workspace_id, function_id, name, description,
 active, created_at, updated_at`

func scanOperatingLoop(row pgx.Row) (OperatingLoop, error) {
	var operatingLoop OperatingLoop
	err := row.Scan(
		&operatingLoop.ID,
		&operatingLoop.WorkspaceID,
		&operatingLoop.FunctionID,
		&operatingLoop.Name,
		&operatingLoop.Description,
		&operatingLoop.Active,
		&operatingLoop.CreatedAt,
		&operatingLoop.UpdatedAt,
	)
	return operatingLoop, err
}

func (s *Store) CreateOperatingLoop(
	ctx context.Context,
	workspaceID uuid.UUID,
	input OperatingLoopInput,
) (OperatingLoop, error) {
	if err := ValidateOperatingLoop(input); err != nil {
		return OperatingLoop{}, err
	}

	operatingLoop, err := scanOperatingLoop(s.pool.QueryRow(ctx, `
		INSERT INTO cerebro_ai_impact_operating_loop
			(workspace_id, function_id, name, description)
		SELECT $1, ai_function.id, $3, $4
		FROM cerebro_ai_impact_function ai_function
		WHERE ai_function.workspace_id = $1 AND ai_function.id = $2
		RETURNING `+operatingLoopColumns,
		workspaceID,
		input.FunctionID,
		input.Name,
		input.Description,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return OperatingLoop{}, ErrNotFound
	}
	return operatingLoop, err
}

func (s *Store) ListOperatingLoops(ctx context.Context, workspaceID uuid.UUID) ([]OperatingLoop, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+operatingLoopColumns+`
		FROM cerebro_ai_impact_operating_loop
		WHERE workspace_id = $1
		ORDER BY name, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	operatingLoops := make([]OperatingLoop, 0)
	for rows.Next() {
		operatingLoop, err := scanOperatingLoop(rows)
		if err != nil {
			return nil, err
		}
		operatingLoops = append(operatingLoops, operatingLoop)
	}
	return operatingLoops, rows.Err()
}

const projectBindingColumns = `id, workspace_id, project_id, operating_loop_id,
 active, created_at, updated_at`

func scanProjectBinding(row pgx.Row) (ProjectBinding, error) {
	var binding ProjectBinding
	err := row.Scan(
		&binding.ID,
		&binding.WorkspaceID,
		&binding.ProjectID,
		&binding.OperatingLoopID,
		&binding.Active,
		&binding.CreatedAt,
		&binding.UpdatedAt,
	)
	return binding, err
}

func (s *Store) CreateProjectBinding(
	ctx context.Context,
	workspaceID uuid.UUID,
	input ProjectBindingInput,
) (ProjectBinding, error) {
	if err := ValidateProjectBinding(input); err != nil {
		return ProjectBinding{}, err
	}

	binding, err := scanProjectBinding(s.pool.QueryRow(ctx, `
		INSERT INTO cerebro_ai_impact_project_binding
			(workspace_id, project_id, operating_loop_id)
		SELECT $1, project.id, operating_loop.id
		FROM project
		JOIN cerebro_ai_impact_operating_loop operating_loop
			ON operating_loop.workspace_id = $1 AND operating_loop.id = $3
		WHERE project.workspace_id = $1 AND project.id = $2
		RETURNING `+projectBindingColumns,
		workspaceID,
		input.ProjectID,
		input.OperatingLoopID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectBinding{}, ErrNotFound
	}
	return binding, err
}

func (s *Store) ListProjectBindings(ctx context.Context, workspaceID uuid.UUID) ([]ProjectBinding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+projectBindingColumns+`
		FROM cerebro_ai_impact_project_binding
		WHERE workspace_id = $1
		ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bindings := make([]ProjectBinding, 0)
	for rows.Next() {
		binding, err := scanProjectBinding(rows)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}

const metricColumns = `id, workspace_id, operating_loop_id, name, family, unit,
 direction, baseline_start, baseline_end, source, guardrail, active, created_at, updated_at`

func scanMetric(row pgx.Row) (Metric, error) {
	var metric Metric
	err := row.Scan(
		&metric.ID,
		&metric.WorkspaceID,
		&metric.OperatingLoopID,
		&metric.Name,
		&metric.Family,
		&metric.Unit,
		&metric.Direction,
		&metric.BaselineStart,
		&metric.BaselineEnd,
		&metric.Source,
		&metric.Guardrail,
		&metric.Active,
		&metric.CreatedAt,
		&metric.UpdatedAt,
	)
	return metric, err
}

func (s *Store) CreateMetric(
	ctx context.Context,
	workspaceID uuid.UUID,
	input MetricInput,
) (Metric, error) {
	if err := ValidateMetric(input); err != nil {
		return Metric{}, err
	}

	metric, err := scanMetric(s.pool.QueryRow(ctx, `
		INSERT INTO cerebro_ai_impact_metric
			(workspace_id, operating_loop_id, name, family, unit, direction,
			 baseline_start, baseline_end, source, guardrail)
		SELECT $1, operating_loop.id, $3, $4, $5, $6, $7, $8, $9, $10
		FROM cerebro_ai_impact_operating_loop operating_loop
		WHERE operating_loop.workspace_id = $1 AND operating_loop.id = $2
		RETURNING `+metricColumns,
		workspaceID,
		input.OperatingLoopID,
		input.Name,
		input.Family,
		input.Unit,
		input.Direction,
		input.BaselineStart,
		input.BaselineEnd,
		input.Source,
		input.Guardrail,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Metric{}, ErrNotFound
	}
	return metric, err
}

func (s *Store) ListMetrics(ctx context.Context, workspaceID uuid.UUID) ([]Metric, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+metricColumns+`
		FROM cerebro_ai_impact_metric
		WHERE workspace_id = $1
		ORDER BY name, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metrics := make([]Metric, 0)
	for rows.Next() {
		metric, err := scanMetric(rows)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, metric)
	}
	return metrics, rows.Err()
}

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

func (s *Store) ListWorkspaceObservations(ctx context.Context, workspaceID uuid.UUID) ([]Observation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+observationColumns+`
		FROM cerebro_ai_impact_observation
		WHERE workspace_id = $1
		ORDER BY created_at, id`, workspaceID)
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
