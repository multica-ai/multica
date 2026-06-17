package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	JobNameKnowledgeGovernance       = "knowledge_governance_sweep"
	JobNameKnowledgeEmbeddingRebuild = "knowledge_embedding_rebuild"
	JobNameKnowledgeEffectRollup     = "rollup_knowledge_effect_hourly"
)

func KnowledgeGovernanceJob(pool *pgxpool.Pool) JobSpec {
	return JobSpec{
		Name:              JobNameKnowledgeGovernance,
		Cadence:           6 * time.Hour,
		ScheduleDelay:     5 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        30 * time.Minute,
		StaleTimeout:      40 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler:           makeKnowledgeGovernanceHandler(pool),
	}
}

func KnowledgeEmbeddingRebuildJob(pool *pgxpool.Pool, curatorEngine service.CuratorEngine) JobSpec {
	return JobSpec{
		Name:              JobNameKnowledgeEmbeddingRebuild,
		Cadence:           30 * time.Minute,
		ScheduleDelay:     2 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     2 * time.Hour,
		RunTimeout:        25 * time.Minute,
		StaleTimeout:      35 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{2 * time.Minute, 10 * time.Minute, 20 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler:           makeKnowledgeEmbeddingRebuildHandler(pool, curatorEngine),
	}
}

func makeKnowledgeGovernanceHandler(pool *pgxpool.Pool) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		queries := db.New(pool)
		workspaceIDs, err := queries.ListAllWorkspaceIDs(ctx)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("list workspaces: %w", err)
		}
		svc := service.NewKnowledgeService(queries, pool)
		checked := 0
		reviewNeeded := 0
		conflicts := 0
		for i, workspaceID := range workspaceIDs {
			result, err := svc.RunGovernance(ctx, service.KnowledgeGovernanceParams{WorkspaceID: workspaceID, Limit: 500})
			if err != nil {
				return HandlerResult{}, fmt.Errorf("run governance for workspace %s: %w", uuidText(workspaceID), err)
			}
			checked += result.Checked
			reviewNeeded += result.ReviewNeeded
			conflicts += result.Conflicts
			if i%10 == 0 && in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
		}
		return HandlerResult{RowsAffected: int64(checked), Result: map[string]any{"workspaces": len(workspaceIDs), "checked": checked, "review_needed": reviewNeeded, "conflicts": conflicts}}, nil
	}
}

func makeKnowledgeEmbeddingRebuildHandler(pool *pgxpool.Pool, curatorEngine service.CuratorEngine) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		queries := db.New(pool)
		workspaceIDs, err := queries.ListAllWorkspaceIDs(ctx)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("list workspaces: %w", err)
		}
		knowledgeSvc := service.NewKnowledgeService(queries, pool)
		curatorSvc := service.NewKnowledgeCuratorService(queries, pool, knowledgeSvc, curatorEngine)
		checked := 0
		rebuilt := 0
		skipped := 0
		failed := 0
		for i, workspaceID := range workspaceIDs {
			result, err := curatorSvc.RebuildKnowledgeEmbeddings(ctx, service.KnowledgeEmbeddingRebuildParams{WorkspaceID: workspaceID, Limit: 100})
			if err != nil {
				if errors.Is(err, service.ErrCuratorEngineUnavailable) {
					failed++
					continue
				}
				return HandlerResult{}, fmt.Errorf("rebuild embeddings for workspace %s: %w", uuidText(workspaceID), err)
			}
			checked += result.Checked
			rebuilt += result.Rebuilt
			skipped += result.Skipped
			failed += result.Failed
			if i%5 == 0 && in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
		}
		return HandlerResult{RowsAffected: int64(rebuilt), Result: map[string]any{"workspaces": len(workspaceIDs), "checked": checked, "rebuilt": rebuilt, "skipped": skipped, "failed": failed}}, nil
	}
}

func uuidText(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", id.Bytes[0:4], id.Bytes[4:6], id.Bytes[6:8], id.Bytes[8:10], id.Bytes[10:16])
}

const knowledgeEffectAdvisoryLockID = 4248

// KnowledgeEffectRollupJob returns the JobSpec for the knowledge_effect_hourly
// rollup. Follows the same pattern as TaskUsageHourlyJob. The handler calls
// the rollup_knowledge_effect_hourly() SQL function which holds advisory
// lock 4248 internally.
func KnowledgeEffectRollupJob(pool *pgxpool.Pool) JobSpec {
	return JobSpec{
		Name:              JobNameKnowledgeEffectRollup,
		Cadence:           5 * time.Minute,
		ScheduleDelay:     5 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        25 * time.Minute,
		StaleTimeout:      30 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff: []time.Duration{
			1 * time.Minute,
			5 * time.Minute,
			15 * time.Minute,
		},
		Scopes:  StaticScopes(ScopeGlobal),
		Handler: makeKnowledgeEffectRollupHandler(pool),
	}
}

func makeKnowledgeEffectRollupHandler(pool *pgxpool.Pool) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		watermarkBefore, err := readKnowledgeEffectWatermark(ctx, pool)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("read knowledge effect watermark before: %w", err)
		}

		var rows int64
		if err := pool.QueryRow(ctx, `SELECT rollup_knowledge_effect_hourly()`).Scan(&rows); err != nil {
			return HandlerResult{}, fmt.Errorf("rollup_knowledge_effect_hourly: %w", err)
		}

		watermarkAfter, err := readKnowledgeEffectWatermark(ctx, pool)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("read knowledge effect watermark after: %w", err)
		}

		if in.Heartbeat != nil {
			_ = in.Heartbeat(ctx)
		}

		result := map[string]any{
			"advisory_lock_id": knowledgeEffectAdvisoryLockID,
		}
		if !watermarkBefore.IsZero() {
			result["watermark_before"] = watermarkBefore.UTC().Format(time.RFC3339)
		}
		if !watermarkAfter.IsZero() {
			result["watermark_after"] = watermarkAfter.UTC().Format(time.RFC3339)
		}
		return HandlerResult{
			RowsAffected: rows,
			Result:       result,
		}, nil
	}
}

func readKnowledgeEffectWatermark(ctx context.Context, pool *pgxpool.Pool) (time.Time, error) {
	var t time.Time
	err := pool.QueryRow(ctx, `
		SELECT watermark_at
		  FROM knowledge_effect_hourly_rollup_state
		 WHERE id = 1
	`).Scan(&t)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return t, nil
}
