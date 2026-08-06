package workflows

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// HookFeature owns the complete workflow-hook surface: API, persistence,
// engine and lifecycle gates. The shared server only mounts and delegates.
type HookFeature struct {
	API            *HookAPI
	Evaluator      HookEvaluator
	CompletionGate *TaskCompletionGate
	FailureGate    *TaskFailureGate
	// AssignGate fires before.issue.assigned when an issue is handed to an
	// agent (FIR-4183). It subscribes to the bus itself — see its Attach.
	AssignGate *IssueAssignGate
}

func NewHookFeature(db cerebrodb.DBTX, policies *toolpolicy.Store, evalStore EvalStore) *HookFeature {
	repository := NewPostgresHookRepository(db)
	engineStore := NewPostgresHookEngineStore(repository)
	authorizer := NewToolPolicyHookAuthorizer(policies)
	actions := NewActionRegistry()
	registerVersionOneHookActions(actions, NewPostgresHookActionExecutor(db, authorizer, evalStore))
	engine := NewHookEngine(hookFeatureEnabled(), engineStore).WithActionRegistry(actions)
	if evalStore != nil {
		// evalStore satisfies HookConditionResolver via its EvalPassed method.
		// Guarded so a nil store does not become a non-nil interface that
		// panics when the eval_passed condition is evaluated.
		engine = engine.WithConditionResolver(evalStore)
	}
	evaluator := NewWorkspaceHookEvaluator(hookFeatureEnabled(), engine).
		WithWorkspaceFlags(NewPostgresWorkspaceFlagResolver(db))
	return &HookFeature{
		API:            NewHookAPI(repository, authorizer),
		Evaluator:      evaluator,
		CompletionGate: NewTaskCompletionGate(NewPostgresTaskCompletionStore(db), evaluator, hookFeatureEnabled()),
		FailureGate:    NewTaskFailureGate(NewPostgresTaskFailureContextStore(db), evaluator, hookFeatureEnabled()),
		AssignGate:     NewIssueAssignGate(evaluator),
	}
}

func hookFeatureEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CEREBRO_WORKFLOW_HOOKS_ENABLED"))) {
	case "", "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
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
