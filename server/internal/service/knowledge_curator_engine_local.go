package service

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// LocalCuratorEngine implements CuratorEngine by dispatching draft generation
// tasks to local daemon runtimes. SummarizeSource and BuildEmbedding delegate
// to a base OpenAI-compatible engine since they are fast, stateless operations
// that don't benefit from local execution.
type LocalCuratorEngine struct {
	queries       *db.Queries
	secretService *WorkspaceSecretService
	draftService  *CuratorDraftTaskService
	base          *OpenAICompatibleCuratorEngine
	secretRef     string
}

// NewLocalCuratorEngine creates a local curator engine. The base engine is
// used for SummarizeSource and BuildEmbedding. GenerateDraft dispatches to
// the daemon via CuratorDraftTaskService.
func NewLocalCuratorEngine(queries *db.Queries, secretService *WorkspaceSecretService, draftService *CuratorDraftTaskService, base OpenAICompatibleCuratorConfig, secretRef string) *LocalCuratorEngine {
	cfg := normalizeOpenAICompatibleCuratorConfig(base)
	client := &http.Client{Timeout: cfg.Timeout}
	return &LocalCuratorEngine{
		queries:       queries,
		secretService: secretService,
		draftService:  draftService,
		base:          &OpenAICompatibleCuratorEngine{cfg: cfg, client: client},
		secretRef:     secretRef,
	}
}

func (e *LocalCuratorEngine) GenerateDraft(ctx context.Context, input CuratorDraftInput) (CuratorDraft, error) {
	workspaceID := input.WorkspaceID

	// Find an online local daemon runtime for this workspace.
	runtimes, err := e.queries.ListOnlineDaemonRuntimes(ctx, workspaceID)
	if err != nil || len(runtimes) == 0 {
		return CuratorDraft{}, ErrCuratorLocalRuntimeUnavailable
	}
	runtime := runtimes[0]

	// Resolve the secret_ref to an actual API key.
	apiKey, err := e.secretService.ResolveSecretRef(ctx, workspaceID, e.secretRef)
	if err != nil {
		return CuratorDraft{}, ErrCuratorSecretNotFound
	}

	// Build the task input with the resolved API key.
	draftKind := "issue"
	candidateID := pgtype.UUID{}
	findingID := pgtype.UUID{}
	regenerate := false
	if input.Candidate != nil {
		draftKind = "candidate"
		candidateID = input.Candidate.ID
	}
	if input.Governance != nil {
		draftKind = "governance_finding"
		findingID = input.Governance.ID
	}

	taskInput := CuratorDraftTaskInput{
		BaseURL:        e.base.cfg.BaseURL,
		APIKey:         apiKey,
		Model:          e.base.cfg.Model,
		EmbeddingModel: e.base.cfg.EmbeddingModel,
		Provider:       e.base.cfg.Provider,
		DraftInput:     input,
		CandidateID:    candidateID,
		FindingID:      findingID,
		Regenerate:     regenerate,
	}

	createdBy := input.Issue.CreatorID
	if input.Candidate != nil && input.Candidate.CreatedBy.Valid {
		createdBy = input.Candidate.CreatedBy
	}

	_, err = e.draftService.EnqueueDraftTask(ctx, workspaceID, runtime.ID, draftKind, taskInput, createdBy)
	if err != nil {
		return CuratorDraft{}, err
	}

	// Return the sentinel error so the caller knows the draft was dispatched.
	return CuratorDraft{}, ErrCuratorDraftDispatched
}

func (e *LocalCuratorEngine) SummarizeSource(ctx context.Context, source CuratorSourceBundle) (string, error) {
	return e.base.SummarizeSource(ctx, source)
}

func (e *LocalCuratorEngine) BuildEmbedding(ctx context.Context, content string) ([]float32, error) {
	return e.base.BuildEmbedding(ctx, content)
}

func (e *LocalCuratorEngine) Info() CuratorEngineInfo {
	return CuratorEngineInfo{
		Provider:       e.base.cfg.Provider,
		Model:          e.base.cfg.Model,
		EmbeddingModel: e.base.cfg.EmbeddingModel,
		RuntimeMode:    "local",
	}
}
