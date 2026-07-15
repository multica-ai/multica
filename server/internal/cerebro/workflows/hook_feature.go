package workflows

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// HookFeature owns the complete workflow-hook surface: API, persistence,
// engine and lifecycle gates. The shared server only mounts and delegates.
type HookFeature struct {
	API            *HookAPI
	CompletionGate *TaskCompletionGate
}

func NewHookFeature(db cerebrodb.DBTX, policies *toolpolicy.Store) *HookFeature {
	repository := NewPostgresHookRepository(db)
	engineStore := NewPostgresHookEngineStore(repository)
	authorizer := NewToolPolicyHookAuthorizer(policies)
	actions := NewActionRegistry()
	registerVersionOneHookActions(actions, NewPostgresHookActionExecutor(db, authorizer))
	engine := NewHookEngine(hookFeatureEnabled(), engineStore).WithActionRegistry(actions)
	return &HookFeature{
		API:            NewHookAPI(repository, authorizer),
		CompletionGate: NewTaskCompletionGate(NewPostgresTaskCompletionStore(db), engine, hookFeatureEnabled()),
	}
}

func hookFeatureEnabled() bool {
	return envFlagEnabled("CEREBRO_WORKFLOW_HOOKS_ENABLED")
}

func (f *HookFeature) Routes() http.Handler {
	return f.API.Routes()
}

type PostgresHookEngineStore struct {
	repository HookRepository
	mu         sync.Mutex
	results    map[string]HookResult
}

func NewPostgresHookEngineStore(repository HookRepository) *PostgresHookEngineStore {
	return &PostgresHookEngineStore{repository: repository, results: make(map[string]HookResult)}
}

func (s *PostgresHookEngineStore) EffectivePolicies(ctx context.Context, event HookEvent) ([]HookPolicy, error) {
	return s.repository.List(ctx, event.WorkspaceID)
}

func (s *PostgresHookEngineStore) GetResult(_ context.Context, key string) (HookResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.results[key]
	return result, ok
}

func (s *PostgresHookEngineStore) SaveResult(ctx context.Context, key string, event HookEvent, result HookResult) error {
	s.mu.Lock()
	if _, exists := s.results[key]; exists {
		s.mu.Unlock()
		return nil
	}
	s.results[key] = result
	s.mu.Unlock()

	recorded := make(map[string]bool)
	for _, match := range result.Matches {
		if recorded[match.PolicyID] {
			continue
		}
		recorded[match.PolicyID] = true
		run := HookRunRecord{
			PolicyID: match.PolicyID, PolicyVersion: match.Version, Event: event,
			Result: result, SourceScope: match.SourceScope, CreatedAt: time.Now().UTC(),
		}
		if err := s.repository.RecordRun(ctx, event.WorkspaceID, run); err != nil {
			return err
		}
	}
	return nil
}
