package aiimpact

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("AI Impact resource not found")

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func peoplePeriodWindow(period PeoplePeriod, now time.Time) (time.Time, string, error) {
	switch period {
	case PeoplePeriodHour:
		return now.Add(-time.Hour), "minute", nil
	case PeoplePeriodDay:
		return now.Add(-24 * time.Hour), "hour", nil
	case PeoplePeriodWeek:
		return now.Add(-7 * 24 * time.Hour), "day", nil
	case PeoplePeriodMonth:
		return now.Add(-30 * 24 * time.Hour), "day", nil
	default:
		return time.Time{}, "", fmt.Errorf("unsupported people period %q", period)
	}
}

// ListPeopleImpact reads direct workspace activity and only exposes sampled
// quality outcomes once at least five measurements exist for that person.
func (s *Store) ListPeopleImpact(
	ctx context.Context,
	workspaceID uuid.UUID,
	period PeoplePeriod,
	now time.Time,
) ([]PersonImpact, error) {
	start, grain, err := peoplePeriodWindow(period, now)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		WITH people AS (
			SELECT u.id, 'member'::text AS person_type, u.name
			FROM member m
			JOIN "user" u ON u.id = m.user_id
			WHERE m.workspace_id = $1
			UNION ALL
			SELECT a.id, 'agent'::text, a.name
			FROM agent a
			WHERE a.workspace_id = $1 AND a.archived_at IS NULL
		),
		person_runs AS (
			SELECT p.id, p.person_type, r.id AS analytics_run_id, r.run_id,
				r.cost_cents, r.project_id
			FROM people p
			JOIN cerebro_analytics_run r ON r.workspace_id = $1
				AND r.started_at >= $2
				AND (
					(p.person_type = 'member' AND r.person_id = p.id)
					OR (p.person_type = 'agent' AND r.agent_id = p.id)
				)
		),
		run_totals AS (
			SELECT id, person_type,
				COUNT(DISTINCT run_id)::bigint AS runs,
				CASE
					WHEN COUNT(*) FILTER (WHERE cost_cents IS NOT NULL) = 0 THEN NULL
					ELSE SUM(cost_cents) FILTER (WHERE cost_cents IS NOT NULL)::bigint
				END AS cost_cents
			FROM person_runs
			GROUP BY id, person_type
		),
		quality_totals AS (
			SELECT pr.id, pr.person_type,
				COUNT(DISTINCT q.analytics_run_id)::bigint AS sample_size,
				AVG(q.score) FILTER (
					WHERE q.measurement_type <> 'satisfaction' AND q.score IS NOT NULL
				) AS solution_quality,
				AVG(q.score) FILTER (
					WHERE q.category ILIKE '%prompt%' AND q.score IS NOT NULL
				) AS prompt_effectiveness,
				AVG(q.confidence) FILTER (WHERE q.confidence IS NOT NULL) AS confidence,
				COUNT(*) FILTER (
					WHERE q.measurement_type = 'satisfaction'
						AND q.verdict IN ('approved', '👍', '❤️', '🎉')
				)::float8
				/ NULLIF(COUNT(*) FILTER (
					WHERE q.measurement_type = 'satisfaction'
						AND q.verdict IN ('approved', 'rejected', '👍', '👎', '❤️', '🎉')
				), 0)::float8 AS frustration_free
			FROM person_runs pr
			JOIN cerebro_analytics_quality_measurement q
				ON q.analytics_run_id = pr.analytics_run_id AND q.workspace_id = $1
			GROUP BY pr.id, pr.person_type
		),
		skill_totals AS (
			SELECT pr.id, pr.person_type, SUM(sk.invocation_count)::bigint AS skill_activity
			FROM person_runs pr
			JOIN cerebro_analytics_run_skill sk
				ON sk.analytics_run_id = pr.analytics_run_id AND sk.workspace_id = $1
			GROUP BY pr.id, pr.person_type
		),
		issue_totals AS (
			SELECT p.id, p.person_type, COUNT(DISTINCT i.id)::bigint AS issues
			FROM people p
			JOIN issue i ON i.workspace_id = $1 AND i.kind = 'issue'
				AND i.created_at >= $2
				AND (
					(i.creator_type = p.person_type AND i.creator_id = p.id)
					OR (i.assignee_type = p.person_type AND i.assignee_id = p.id)
				)
			GROUP BY p.id, p.person_type
		),
		project_totals AS (
			SELECT id, person_type, COUNT(DISTINCT project_id)::bigint AS projects
			FROM (
				SELECT p.id, p.person_type, project.id AS project_id
				FROM people p
				JOIN project ON project.workspace_id = $1 AND project.created_at >= $2
					AND project.lead_type = p.person_type AND project.lead_id = p.id
				UNION ALL
				SELECT id, person_type, project_id
				FROM person_runs
				WHERE project_id IS NOT NULL
			) person_projects
			GROUP BY id, person_type
		),
		chat_totals AS (
			SELECT p.id, p.person_type, COUNT(DISTINCT cs.id)::bigint AS chats
			FROM people p
			JOIN chat_session cs ON cs.workspace_id = $1 AND cs.created_at >= $2
				AND (
					(p.person_type = 'member' AND cs.creator_id = p.id)
					OR (p.person_type = 'agent' AND cs.agent_id = p.id)
				)
			GROUP BY p.id, p.person_type
		),
		channel_totals AS (
			SELECT p.id, p.person_type, COUNT(DISTINCT i.id)::bigint AS channels
			FROM people p
			JOIN issue i ON i.workspace_id = $1 AND i.kind IN ('channel', 'group')
				AND i.created_at >= $2
			LEFT JOIN issue_subscriber subscriber
				ON subscriber.issue_id = i.id AND subscriber.user_id = p.id
			WHERE (i.creator_type = p.person_type AND i.creator_id = p.id)
				OR subscriber.user_id IS NOT NULL
			GROUP BY p.id, p.person_type
		)
		SELECT p.id, p.person_type, p.name,
			COALESCE(rt.runs, 0), COALESCE(it.issues, 0),
			COALESCE(pt.projects, 0),
			COALESCE(ct.chats, 0), COALESCE(cht.channels, 0),
			COALESCE(st.skill_activity, 0), rt.cost_cents,
			CASE WHEN COALESCE(qt.sample_size, 0) >= 5 THEN qt.solution_quality END,
			CASE WHEN COALESCE(qt.sample_size, 0) >= 5 THEN qt.frustration_free END,
			CASE WHEN COALESCE(qt.sample_size, 0) >= 5 THEN qt.prompt_effectiveness END,
			CASE WHEN COALESCE(qt.sample_size, 0) >= 5 THEN qt.confidence END,
			CASE WHEN COALESCE(qt.sample_size, 0) >= 5 THEN qt.sample_size ELSE 0 END
		FROM people p
		LEFT JOIN run_totals rt ON rt.id = p.id AND rt.person_type = p.person_type
		LEFT JOIN quality_totals qt ON qt.id = p.id AND qt.person_type = p.person_type
		LEFT JOIN skill_totals st ON st.id = p.id AND st.person_type = p.person_type
		LEFT JOIN issue_totals it ON it.id = p.id AND it.person_type = p.person_type
		LEFT JOIN project_totals pt ON pt.id = p.id AND pt.person_type = p.person_type
		LEFT JOIN chat_totals ct ON ct.id = p.id AND ct.person_type = p.person_type
		LEFT JOIN channel_totals cht ON cht.id = p.id AND cht.person_type = p.person_type
		ORDER BY COALESCE(rt.runs, 0) DESC, p.person_type, p.name, p.id`,
		workspaceID, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	people := make([]PersonImpact, 0)
	byKey := make(map[string]int)
	for rows.Next() {
		var person PersonImpact
		var cost sql.NullInt64
		var solutionQuality, frustrationFree, promptEffectiveness, confidence sql.NullFloat64
		if err := rows.Scan(
			&person.ID, &person.Type, &person.Name,
			&person.Usage.Runs, &person.Usage.Issues, &person.Usage.Projects,
			&person.Usage.Chats, &person.Usage.Channels,
			&person.Outcomes.SkillActivity, &cost,
			&solutionQuality, &frustrationFree, &promptEffectiveness, &confidence,
			&person.SampleSize,
		); err != nil {
			return nil, err
		}
		person.Activity = make([]PeopleActivityBucket, 0)
		if cost.Valid {
			person.Outcomes.CostCents = &cost.Int64
		}
		if solutionQuality.Valid {
			person.Outcomes.SolutionQuality = &solutionQuality.Float64
		}
		if frustrationFree.Valid {
			person.Outcomes.FrustrationFree = &frustrationFree.Float64
		}
		if promptEffectiveness.Valid {
			person.Outcomes.PromptEffectiveness = &promptEffectiveness.Float64
		}
		if confidence.Valid {
			person.Confidence = &confidence.Float64
		}
		key := person.Type + ":" + person.ID.String()
		byKey[key] = len(people)
		people = append(people, person)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	activityRows, err := s.pool.Query(ctx, `
		SELECT person_type, person_id, date_trunc($3::text, started_at) AS bucket,
			COUNT(DISTINCT run_id)::bigint
		FROM (
			SELECT 'member'::text AS person_type, person_id, started_at, run_id
			FROM cerebro_analytics_run
			WHERE workspace_id = $1 AND started_at >= $2 AND person_id IS NOT NULL
			UNION ALL
			SELECT 'agent'::text, agent_id, started_at, run_id
			FROM cerebro_analytics_run
			WHERE workspace_id = $1 AND started_at >= $2 AND agent_id IS NOT NULL
		) activity
		GROUP BY person_type, person_id, date_trunc($3::text, started_at)
		ORDER BY bucket`, workspaceID, start, grain)
	if err != nil {
		return nil, err
	}
	defer activityRows.Close()
	for activityRows.Next() {
		var personType string
		var personID uuid.UUID
		var bucket PeopleActivityBucket
		if err := activityRows.Scan(&personType, &personID, &bucket.Bucket, &bucket.Count); err != nil {
			return nil, err
		}
		if index, ok := byKey[personType+":"+personID.String()]; ok {
			people[index].Activity = append(people[index].Activity, bucket)
		}
	}
	return people, activityRows.Err()
}

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
 direction, baseline_start, baseline_end, target_value, source, guardrail, active, created_at, updated_at`

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
		&metric.TargetValue,
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
