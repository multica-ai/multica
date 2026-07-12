package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type loadRunFunc func(context.Context, string) (RunProjection, error)
type listRunIDsFunc func(context.Context, string, string, int) ([]string, error)

type PostgresProjectionSource struct {
	load loadRunFunc
	list listRunIDsFunc
}

func NewPostgresProjectionSource(pool *pgxpool.Pool) *PostgresProjectionSource {
	s := &postgresSourceDB{pool: pool}
	return newPostgresProjectionSourceWithLoader(s.loadRun, s.listRunIDs)
}

func newPostgresProjectionSourceWithLoader(load loadRunFunc, list listRunIDsFunc) *PostgresProjectionSource {
	return &PostgresProjectionSource{load: load, list: list}
}

func (s *PostgresProjectionSource) LoadRun(ctx context.Context, runID string) (RunProjection, error) {
	if s == nil || s.load == nil {
		return RunProjection{}, fmt.Errorf("postgres analytics source is not configured")
	}
	run, err := s.load(ctx, runID)
	if err != nil {
		return RunProjection{}, fmt.Errorf("load postgres analytics source: %w", err)
	}
	return run, nil
}

func (s *PostgresProjectionSource) ListRunIDs(ctx context.Context, workspaceID, afterCursor string, limit int) ([]string, error) {
	if s == nil || s.list == nil {
		return nil, fmt.Errorf("postgres analytics source is not configured")
	}
	return s.list(ctx, workspaceID, afterCursor, limit)
}

type postgresSourceDB struct{ pool *pgxpool.Pool }

func (s *postgresSourceDB) loadRun(ctx context.Context, runID string) (RunProjection, error) {
	var r RunProjection
	err := s.pool.QueryRow(ctx, `
		SELECT atq.id::text, COALESCE(i.workspace_id,cs.workspace_id,a.workspace_id)::text, 'agent',
		CASE WHEN i.kind IN ('dm','channel') THEN i.kind WHEN atq.chat_session_id IS NOT NULL THEN 'chat' WHEN atq.autopilot_run_id IS NOT NULL THEN 'autopilot' WHEN atq.issue_id IS NOT NULL THEN 'issue' ELSE 'manual' END,
		COALESCE(i.id::text,cs.id::text,''), COALESCE(i.title,cs.title,''),
		COALESCE(person.id::text,''), COALESCE(person.name,''), a.id::text, a.name,
		COALESCE(p.id::text,''), COALESCE(p.title,''), COALESCE(ar.id::text,''), COALESCE(NULLIF(ar.name,''),ar.provider,''),
		atq.status, COALESCE(atq.started_at,atq.created_at), atq.completed_at,
		GREATEST(0,EXTRACT(EPOCH FROM (COALESCE(atq.completed_at,now())-COALESCE(atq.started_at,atq.created_at)))::bigint),
		COALESCE(usage.input_tokens,0),COALESCE(usage.output_tokens,0),COALESCE(usage.cache_read_tokens,0),COALESCE(usage.cache_write_tokens,0),
		usage.cost_cents,COALESCE(usage.provider,''),COALESCE(usage.model,''),atq.id::text
		FROM agent_task_queue atq
		JOIN agent a ON a.id=atq.agent_id
		LEFT JOIN issue i ON i.id=atq.issue_id
		LEFT JOIN project p ON p.id=i.project_id
		LEFT JOIN agent_runtime ar ON ar.id=atq.runtime_id
		LEFT JOIN chat_session cs ON cs.id=atq.chat_session_id
		LEFT JOIN comment trigger_comment ON trigger_comment.id=atq.trigger_comment_id
		LEFT JOIN "user" person ON person.id=COALESCE(atq.initiator_user_id,CASE WHEN trigger_comment.author_type='member' THEN trigger_comment.author_id END,cs.creator_id,CASE WHEN i.creator_type='member' THEN i.creator_id END)
		LEFT JOIN LATERAL (SELECT SUM(input_tokens)::bigint input_tokens,SUM(output_tokens)::bigint output_tokens,SUM(cache_read_tokens)::bigint cache_read_tokens,SUM(cache_write_tokens)::bigint cache_write_tokens,CASE WHEN COUNT(*)>0 THEN COALESCE(SUM(cost_cents),0)::bigint END cost_cents,MAX(provider) provider,MAX(model) model FROM task_usage WHERE task_id=atq.id) usage ON true
		WHERE atq.id=$1`, runID).Scan(
		&r.RunID, &r.WorkspaceID, &r.Population, &r.SourceType, &r.SourceID, &r.SourceLabel,
		&r.PersonID, &r.PersonLabel, &r.AgentID, &r.AgentLabel, &r.ProjectID, &r.ProjectLabel, &r.RuntimeID, &r.RuntimeLabel,
		&r.Status, &r.StartedAt, &r.CompletedAt, &r.DurationSeconds,
		&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens, &r.CostCents, &r.Provider, &r.Model, &r.TraceID)
	if err != nil {
		return RunProjection{}, err
	}
	if r.SourceID != "" {
		href := "/issues/" + r.SourceID + "?run=" + r.RunID
		kind := r.SourceType
		if r.SourceType == "chat" {
			href = "/chat-sessions/" + r.SourceID + "?run=" + r.RunID
		} else if r.SourceType != "issue" && r.SourceType != "dm" && r.SourceType != "channel" {
			kind = "issue"
		}
		r.References = append(r.References, ReferenceProjection{Kind: kind, ID: r.SourceID, Label: r.SourceLabel, Href: href})
	}
	r.References = append(r.References, ReferenceProjection{Kind: "trace", ID: r.TraceID, Label: "Trace"})

	skillRows, err := s.pool.Query(ctx, `SELECT COALESCE(skill_id::text,''),skill_name,invocation_count,first_used_at,last_used_at FROM task_skill_usage WHERE task_id=$1 ORDER BY skill_name`, runID)
	if err != nil {
		return RunProjection{}, err
	}
	defer skillRows.Close()
	for skillRows.Next() {
		var skill SkillProjection
		if err := skillRows.Scan(&skill.ID, &skill.Name, &skill.InvocationCount, &skill.FirstUsedAt, &skill.LastUsedAt); err != nil {
			return RunProjection{}, err
		}
		r.Skills = append(r.Skills, skill)
	}
	if err := skillRows.Err(); err != nil {
		return RunProjection{}, err
	}

	rows, err := s.pool.Query(ctx, `SELECT saving_key, mode, applied, metric, baseline_value, effective_value, saved_cents, created_at FROM cerebro_cost_optimization_measurement WHERE task_id=$1 ORDER BY saving_key`, runID)
	if err != nil {
		return RunProjection{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var v SavingProjection
		if err := rows.Scan(&v.Type, &v.Mode, &v.Applied, &v.Metric, &v.BaselineValue, &v.EffectiveValue, &v.SavedCents, &v.MeasuredAt); err != nil {
			return RunProjection{}, err
		}
		r.Savings = append(r.Savings, v)
	}
	if err := rows.Err(); err != nil {
		return RunProjection{}, err
	}

	qualityRows, err := s.pool.Query(ctx, `SELECT 'skill_observation', so.delta_type, so.outcome, so.confidence, so.skill_version, so.created_at FROM skill_observation so WHERE so.run_id=$1 ORDER BY so.created_at, so.id`, runID)
	if err != nil {
		return RunProjection{}, err
	}
	defer qualityRows.Close()
	for qualityRows.Next() {
		var v QualityProjection
		if err := qualityRows.Scan(&v.Type, &v.Category, &v.Verdict, &v.Confidence, &v.EvaluatorVersion, &v.MeasuredAt); err != nil {
			return RunProjection{}, err
		}
		r.Quality = append(r.Quality, v)
	}
	return r, qualityRows.Err()
}

func (s *postgresSourceDB) listRunIDs(ctx context.Context, workspaceID, afterCursor string, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT atq.id::text FROM agent_task_queue atq JOIN agent a ON a.id=atq.agent_id LEFT JOIN issue i ON i.id=atq.issue_id LEFT JOIN chat_session cs ON cs.id=atq.chat_session_id WHERE COALESCE(i.workspace_id,cs.workspace_id,a.workspace_id)=$1 AND ($2='' OR atq.id>$2::uuid) ORDER BY atq.id LIMIT $3`, workspaceID, afterCursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type PostgresProjectionStore struct{ pool *pgxpool.Pool }

func NewPostgresProjectionStore(pool *pgxpool.Pool) *PostgresProjectionStore {
	return &PostgresProjectionStore{pool: pool}
}

func (s *PostgresProjectionStore) UpsertRun(ctx context.Context, r RunProjection) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("postgres analytics store is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var analyticsRunID string
	err = tx.QueryRow(ctx, `INSERT INTO cerebro_analytics_run (workspace_id,run_id,population,source_type,source_id,source_label,person_id,person_label,agent_id,agent_label,project_id,project_label,runtime_id,runtime_label,status,started_at,completed_at,duration_seconds,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,cost_cents,cost_kind,provider,model,trace_id,projected_at) VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,''),NULLIF($7,'')::uuid,NULLIF($8,''),NULLIF($9,'')::uuid,NULLIF($10,''),NULLIF($11,'')::uuid,NULLIF($12,''),NULLIF($13,'')::uuid,NULLIF($14,''),$15,$16,$17,$18,$19,$20,$21,$22,$23,CASE WHEN $23::bigint IS NULL THEN 'missing' ELSE 'actual' END,NULLIF($24,''),NULLIF($25,''),NULLIF($26,''),now()) ON CONFLICT (workspace_id,run_id) DO UPDATE SET population=EXCLUDED.population,source_type=EXCLUDED.source_type,source_id=EXCLUDED.source_id,source_label=EXCLUDED.source_label,person_id=EXCLUDED.person_id,person_label=EXCLUDED.person_label,agent_id=EXCLUDED.agent_id,agent_label=EXCLUDED.agent_label,project_id=EXCLUDED.project_id,project_label=EXCLUDED.project_label,runtime_id=EXCLUDED.runtime_id,runtime_label=EXCLUDED.runtime_label,status=EXCLUDED.status,started_at=EXCLUDED.started_at,completed_at=EXCLUDED.completed_at,duration_seconds=EXCLUDED.duration_seconds,input_tokens=EXCLUDED.input_tokens,output_tokens=EXCLUDED.output_tokens,cache_read_tokens=EXCLUDED.cache_read_tokens,cache_write_tokens=EXCLUDED.cache_write_tokens,cost_cents=EXCLUDED.cost_cents,cost_kind=EXCLUDED.cost_kind,provider=EXCLUDED.provider,model=EXCLUDED.model,trace_id=EXCLUDED.trace_id,projected_at=now() RETURNING id::text`, r.WorkspaceID, r.RunID, r.Population, r.SourceType, r.SourceID, r.SourceLabel, r.PersonID, r.PersonLabel, r.AgentID, r.AgentLabel, r.ProjectID, r.ProjectLabel, r.RuntimeID, r.RuntimeLabel, r.Status, r.StartedAt, r.CompletedAt, r.DurationSeconds, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.CostCents, r.Provider, r.Model, r.TraceID).Scan(&analyticsRunID)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM cerebro_analytics_run_saving WHERE analytics_run_id=$1`, analyticsRunID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM cerebro_analytics_quality_measurement WHERE analytics_run_id=$1`, analyticsRunID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM cerebro_analytics_run_skill WHERE analytics_run_id=$1`, analyticsRunID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM cerebro_analytics_reference WHERE analytics_run_id=$1`, analyticsRunID); err != nil {
		return err
	}
	for _, skill := range r.Skills {
		if _, err = tx.Exec(ctx, `INSERT INTO cerebro_analytics_run_skill(analytics_run_id,workspace_id,skill_id,skill_name,invocation_count,first_used_at,last_used_at) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7)`, analyticsRunID, r.WorkspaceID, skill.ID, skill.Name, skill.InvocationCount, skill.FirstUsedAt, skill.LastUsedAt); err != nil {
			return err
		}
	}
	for _, reference := range r.References {
		if _, err = tx.Exec(ctx, `INSERT INTO cerebro_analytics_reference(workspace_id,analytics_run_id,reference_kind,reference_id,label,href) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''))`, r.WorkspaceID, analyticsRunID, reference.Kind, reference.ID, reference.Label, reference.Href); err != nil {
			return err
		}
	}
	for _, v := range r.Savings {
		savedTokens := v.BaselineValue - v.EffectiveValue
		if savedTokens < 0 {
			savedTokens = 0
		}
		if v.MeasuredAt.IsZero() {
			v.MeasuredAt = time.Now()
		}
		if _, err = tx.Exec(ctx, `INSERT INTO cerebro_analytics_run_saving (analytics_run_id,workspace_id,saving_type,mode,applied,metric,baseline_value,effective_value,saved_tokens,saved_cents,measured_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, analyticsRunID, r.WorkspaceID, v.Type, v.Mode, v.Applied, v.Metric, v.BaselineValue, v.EffectiveValue, savedTokens, v.SavedCents, v.MeasuredAt); err != nil {
			return err
		}
	}
	for _, v := range r.Quality {
		if v.MeasuredAt.IsZero() {
			v.MeasuredAt = time.Now()
		}
		version := v.EvaluatorVersion
		if version == "" {
			version = "unknown"
		}
		if _, err = tx.Exec(ctx, `INSERT INTO cerebro_analytics_quality_measurement (analytics_run_id,workspace_id,measurement_type,category,verdict,score,confidence,evaluator_version,measured_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (analytics_run_id,measurement_type,category,evaluator_version) DO UPDATE SET verdict=EXCLUDED.verdict,score=EXCLUDED.score,confidence=EXCLUDED.confidence,measured_at=EXCLUDED.measured_at`, analyticsRunID, r.WorkspaceID, v.Type, v.Category, v.Verdict, v.Score, v.Confidence, version, v.MeasuredAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
