package runservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
	evalrunner "github.com/multica-ai/multica/server/internal/cerebro/evals/runner"
	cerebroruntime "github.com/multica-ai/multica/server/internal/cerebro/runtime"
)

type CompleterResolver func(ctx context.Context, workspaceID uuid.UUID, defaultModel string) (evalrunner.Completer, error)

type Executor struct {
	resolveCompleter CompleterResolver
	now              func() time.Time
}

func New(pool *pgxpool.Pool) *Executor {
	return NewWithResolver(workspaceCompleterResolver(pool), time.Now)
}

func NewWithResolver(resolve CompleterResolver, now func() time.Time) *Executor {
	if now == nil {
		now = time.Now
	}
	return &Executor{resolveCompleter: resolve, now: now}
}

type runnerConfig struct {
	Model        string `json:"model"`
	DefaultModel string `json:"default_model"`
}

func (e *Executor) Execute(ctx context.Context, definition evals.Eval) (evals.RunExecution, error) {
	if e == nil || e.resolveCompleter == nil {
		return evals.RunExecution{}, errors.New("eval runner has no completer resolver")
	}
	tasks, err := evals.ParseTasks(definition.Datasets)
	if err != nil {
		return evals.RunExecution{}, fmt.Errorf("parse datasets: %w", err)
	}
	targetSpec, err := evals.ParseTarget(definition.Target)
	if err != nil {
		return evals.RunExecution{}, fmt.Errorf("parse target: %w", err)
	}
	graders, err := evals.ParseGraders(definition.Graders)
	if err != nil {
		return evals.RunExecution{}, fmt.Errorf("parse graders: %w", err)
	}
	if len(graders) != 1 {
		return evals.RunExecution{}, fmt.Errorf("eval runner requires exactly one grader, got %d", len(graders))
	}
	config, err := parseRunnerConfig(definition.Runner)
	if err != nil {
		return evals.RunExecution{}, err
	}
	defaultModel := config.Model
	if defaultModel == "" {
		defaultModel = config.DefaultModel
	}
	completer, err := e.resolveCompleter(ctx, definition.WorkspaceID, defaultModel)
	if err != nil {
		return evals.RunExecution{}, fmt.Errorf("resolve completer: %w", err)
	}
	target, err := evalrunner.NewTarget(completer, targetSpec)
	if err != nil {
		return evals.RunExecution{}, fmt.Errorf("build target: %w", err)
	}
	grader, err := evalrunner.NewGrader(completer, graders[0])
	if err != nil {
		return evals.RunExecution{}, fmt.Errorf("build grader: %w", err)
	}

	startedAt := e.now()
	report := evalrunner.New(target, grader, e.now).Execute(ctx, tasks, evals.ParseThresholdPolicy(definition.Thresholds))
	completedAt := e.now()
	results, err := report.ResultsJSON()
	if err != nil {
		return evals.RunExecution{}, fmt.Errorf("encode runner report: %w", err)
	}
	return evals.RunExecution{
		TargetVersion: targetVersion(targetSpec, definition.Version),
		Status:        report.Outcome.Status,
		Results:       results,
		CostCents:     report.CostCents,
		LatencyMS:     report.LatencyMS,
		StartedAt:     &startedAt,
		CompletedAt:   &completedAt,
	}, nil
}

func parseRunnerConfig(raw json.RawMessage) (runnerConfig, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var config runnerConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return runnerConfig{}, fmt.Errorf("parse runner config: %w", err)
	}
	config.Model = strings.TrimSpace(config.Model)
	config.DefaultModel = strings.TrimSpace(config.DefaultModel)
	return config, nil
}

func targetVersion(spec evals.TargetSpec, evalVersion string) string {
	for _, value := range []string{spec.Ref, spec.Model, evalVersion} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "unknown"
}

func workspaceCompleterResolver(pool *pgxpool.Pool) CompleterResolver {
	return func(ctx context.Context, workspaceID uuid.UUID, defaultModel string) (evalrunner.Completer, error) {
		if pool == nil {
			return nil, errors.New("eval runner has no database")
		}
		var settings []byte
		if err := pool.QueryRow(ctx, `SELECT settings FROM workspace WHERE id=$1`, workspaceID).Scan(&settings); err != nil {
			return nil, fmt.Errorf("load workspace gateway settings: %w", err)
		}
		fallback, err := cerebroruntime.LoadFirtalGatewayRuntimeConfig()
		if err != nil {
			return nil, err
		}
		config, enabled, err := cerebroruntime.FirtalGatewayConfigFromWorkspaceSettings(settings, fallback)
		if err != nil {
			return nil, err
		}
		if !enabled {
			return nil, errors.New("Firtal Gateway is not configured for this workspace")
		}
		if strings.TrimSpace(defaultModel) == "" {
			defaultModel = config.Model
		}
		return evalrunner.NewGatewayCompleter(config.BaseURL, config.APIKey, defaultModel, nil)
	}
}
