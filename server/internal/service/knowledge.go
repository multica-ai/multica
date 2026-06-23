package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	pgvector "github.com/pgvector/pgvector-go"
)

const DefaultKnowledgeEmbeddingDimensions = 1536
const KnowledgeEmbeddingDimensions = DefaultKnowledgeEmbeddingDimensions

var SupportedKnowledgeEmbeddingDimensions = []int{1536, 3072, 1024, 768}

var (
	knowledgeSlugNonAlnum     = regexp.MustCompile(`[^a-z0-9]+`)
	knowledgeReferenceUUIDRe  = regexp.MustCompile(`(?i)\b(?:KNO-)?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\b`)
	knowledgeUsedLineKeywords = []string{"used knowledge", "knowledge used"}
)

var (
	ErrKnowledgeValidation = errors.New("knowledge validation failed")
	ErrKnowledgeNotFound   = errors.New("knowledge item not found")
)

type KnowledgeService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Embedder  Embedder
}

type Embedder interface {
	BuildEmbedding(ctx context.Context, content string) ([]float32, error)
}

func NewKnowledgeService(q *db.Queries, tx TxStarter) *KnowledgeService {
	return &KnowledgeService{Queries: q, TxStarter: tx}
}

type KnowledgeSourceInput struct {
	SourceType    string
	SourceID      pgtype.UUID
	SourceURL     pgtype.Text
	SourceTitle   pgtype.Text
	SourceExcerpt pgtype.Text
}

type KnowledgeCreateParams struct {
	WorkspaceID         pgtype.UUID
	ProjectID           pgtype.UUID
	AgentID             pgtype.UUID
	Title               string
	Type                string
	DomainLabels        []string
	ProblemPattern      string
	TriggerConditions   string
	DiagnosticSteps     string
	RecommendedPractice string
	AntiPatterns        string
	Applicability       string
	ConfidenceStatus    string
	LifecycleStatus     string
	CreatedBy           pgtype.UUID
	Sources             []KnowledgeSourceInput
}

type KnowledgeUpdateParams struct {
	ID                  pgtype.UUID
	WorkspaceID         pgtype.UUID
	ProjectID           pgtype.UUID
	AgentID             pgtype.UUID
	Title               pgtype.Text
	Type                pgtype.Text
	DomainLabels        []string
	DomainLabelsSet     bool
	ProblemPattern      pgtype.Text
	TriggerConditions   pgtype.Text
	DiagnosticSteps     pgtype.Text
	RecommendedPractice pgtype.Text
	AntiPatterns        pgtype.Text
	Applicability       pgtype.Text
	ConfidenceStatus    pgtype.Text
	LifecycleStatus     pgtype.Text
	ReviewedBy          pgtype.UUID
	UpdatedBy           pgtype.UUID
}

type KnowledgeSearchFilters struct {
	ProjectID pgtype.UUID
	AgentID   pgtype.UUID
	Labels    []string
	Types     []string
	Statuses  []string
}

type KnowledgeSearchParams struct {
	WorkspaceID pgtype.UUID
	MemberID    pgtype.UUID
	AgentTaskID pgtype.UUID
	Query       string
	QuerySource string
	Embedding   []float32
	Limit       int32
	Issue       *db.Issue
	Filters     KnowledgeSearchFilters
}

type KnowledgeSearchResult struct {
	Item          db.KnowledgeItem
	SourceSummary KnowledgeSourceSummary
	TextScore     float64
	VectorScore   float64
	FinalScore    float64
	MatchReason   string
}

type KnowledgeTaskClaimParams struct {
	WorkspaceID             pgtype.UUID
	AgentTaskID             pgtype.UUID
	AgentID                 pgtype.UUID
	MemberID                pgtype.UUID
	Issue                   *db.Issue
	IssueIdentifier         string
	IssueLabels             []string
	ProjectTitle            string
	ProjectResources        []KnowledgeTaskProjectResource
	TriggerCommentContent   string
	NewCommentCount         int
	NewCommentsSince        string
	ChatMessage             string
	AutopilotTitle          string
	AutopilotDescription    string
	AutopilotSource         string
	AutopilotTriggerPayload string
	QuickCreatePrompt       string
	LastTaskResult          []byte
	LastTaskError           string
	LastTaskFailureReason   string
	Limit                   int32
	TypeFilters             []string
	ConfidenceThreshold     string
	TokenBudget             int32
}

type KnowledgeTaskProjectResource struct {
	ResourceType string
	Label        string
}

type KnowledgeContextItem struct {
	ID                string  `json:"id"`
	Title             string  `json:"title"`
	Summary           string  `json:"summary,omitempty"`
	RecommendedAction string  `json:"recommended_action,omitempty"`
	AntiPatterns      string  `json:"anti_patterns,omitempty"`
	SourceIssue       string  `json:"source_issue,omitempty"`
	Score             float64 `json:"score"`
	Reason            string  `json:"reason"`
}

type KnowledgeDetail struct {
	Item            db.KnowledgeItem
	Sources         []db.KnowledgeSource
	SourceSummary   KnowledgeSourceSummary
	PublishTargets  []db.KnowledgePublishTarget
	Embeddings      []db.ListKnowledgeEmbeddingMetadataRow
	EmbeddingStatus *db.KnowledgeEmbeddingAttempt
	FeedbackSummary []db.GetKnowledgeFeedbackSummaryRow
}

type KnowledgeSourceSummary struct {
	Count              int
	Types              []string
	PrimarySourceType  string
	PrimarySourceID    pgtype.UUID
	PrimarySourceTitle string
}

type KnowledgeSourceDetail struct {
	Source        db.KnowledgeSource
	ResolvedTitle pgtype.Text
	ResolvedURL   pgtype.Text
	ResolvedNote  pgtype.Text
}

type KnowledgePublishWikiParams struct {
	WorkspaceID pgtype.UUID
	ItemID      pgtype.UUID
	ActorID     pgtype.UUID
	ActorUserID pgtype.UUID
	WikiPageID  pgtype.UUID
	ParentID    pgtype.UUID
	Title       string
	Content     string
}

type KnowledgePublishSkillParams struct {
	WorkspaceID      pgtype.UUID
	ItemID           pgtype.UUID
	ActorID          pgtype.UUID
	ActorUserID      pgtype.UUID
	SkillID          pgtype.UUID
	Name             string
	Description      string
	Content          string
	IncludeSourceMap bool
	SupportingFiles  []KnowledgeSkillFileInput
}

type KnowledgeSkillFileInput struct {
	Path    string
	Content string
}

type KnowledgeFeedbackParams struct {
	KnowledgeItemID pgtype.UUID
	WorkspaceID     pgtype.UUID
	MemberID        pgtype.UUID
	AgentTaskID     pgtype.UUID
	Value           string
	Note            pgtype.Text
}

type KnowledgeUsageParams struct {
	WorkspaceID     pgtype.UUID
	KnowledgeItemID pgtype.UUID
	AgentTaskID     pgtype.UUID
	UsageSource     string
	ReferenceText   string
	TaskStatus      string
	TaskResult      []byte
}

type KnowledgeAnalyticsParams struct {
	WorkspaceID     pgtype.UUID
	KnowledgeItemID pgtype.UUID
	ProjectID       pgtype.UUID
	AgentID         pgtype.UUID
	Since           time.Time
	Until           time.Time
	IncludeZero     bool
	Limit           int32
	Offset          int32
}

type KnowledgeEffectParams struct {
	WorkspaceID  pgtype.UUID
	AgentID      pgtype.UUID
	ProjectID    pgtype.UUID
	TaskKind     pgtype.Text
	HasInjection pgtype.Bool
	Model        pgtype.Text
	Since        time.Time
	Until        time.Time
	Limit        int32
	Offset       int32
}

type KnowledgeEffectSummaryParams struct {
	WorkspaceID  pgtype.UUID
	AgentID      pgtype.UUID
	ProjectID    pgtype.UUID
	TaskKind     pgtype.Text
	HasInjection pgtype.Bool
	Model        pgtype.Text
	Since        time.Time
	Until        time.Time
}

type KnowledgeGovernanceParams struct {
	WorkspaceID pgtype.UUID
	Limit       int32
}

type KnowledgeGovernanceResult struct {
	Checked      int
	ReviewNeeded int
	Conflicts    int
	Findings     int
}

type KnowledgeGovernanceFindingListParams struct {
	WorkspaceID     pgtype.UUID
	Status          string
	FindingType     string
	KnowledgeItemID pgtype.UUID
	Limit           int32
	Offset          int32
}

type KnowledgeGovernanceFindingActionParams struct {
	WorkspaceID pgtype.UUID
	FindingID   pgtype.UUID
	ActorID     pgtype.UUID
	Action      string
}

type KnowledgeGovernanceFindingInput struct {
	FindingType     string
	Severity        int32
	Reason          string
	SuggestedAction string
	Evidence        map[string]any
	SourceMap       map[string]any
}

type KnowledgeCandidateEvaluateParams struct {
	WorkspaceID    pgtype.UUID
	SourceType     string
	SourceID       pgtype.UUID
	TriggerReason  string
	Manual         bool
	CreatedBy      pgtype.UUID
	AgentTask      *db.AgentTaskQueue
	TaskResult     []byte
	Issue          *db.Issue
	Comment        *db.Comment
	AdditionalMeta map[string]any
}

type candidateSource struct {
	Issue       db.Issue
	CommentID   pgtype.UUID
	AgentTaskID pgtype.UUID
}

type skipRuleCheck struct {
	Evaluated   bool        `json:"evaluated"`
	MatchedRule interface{} `json:"matched_rule"`
}

type retryFailure struct {
	Attempt      int    `json:"attempt"`
	Status       string `json:"status"`
	ErrorSummary string `json:"error_summary"`
}

type retrySuccess struct {
	Attempt int    `json:"attempt"`
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
}

type retryChainEvidence struct {
	TotalAttempts int            `json:"total_attempts"`
	HasClearError bool           `json:"has_clear_error"`
	Failures      []retryFailure `json:"failures"`
	FinalSuccess  *retrySuccess  `json:"final_success"`
}

type correctionRound struct {
	CommentID   string `json:"comment_id"`
	CommentText string `json:"comment_text"`
	HadFollowUp bool   `json:"had_follow_up"`
}

type prEvidenceItem struct {
	Number       int32  `json:"number"`
	Title        string `json:"title"`
	RepoOwner    string `json:"repo_owner"`
	RepoName     string `json:"repo_name"`
	State        string `json:"state"`
	MergedAt     string `json:"merged_at"`
	Additions    int32  `json:"additions"`
	Deletions    int32  `json:"deletions"`
	ChangedFiles int32  `json:"changed_files"`
}

type similarityMatch struct {
	KnowledgeItemID string  `json:"knowledge_item_id"`
	Title           string  `json:"title"`
	VectorScore     float64 `json:"vector_score"`
}

func (s *KnowledgeService) List(ctx context.Context, arg db.ListKnowledgeItemsParams) ([]db.KnowledgeItem, error) {
	if err := validateOptionalKnowledgeFilters(ctx, s.Queries, arg.WorkspaceID, arg.ProjectID, arg.AgentID); err != nil {
		return nil, err
	}
	if arg.Type.Valid && !validKnowledgeType(arg.Type.String) {
		return nil, validationError("invalid type")
	}
	if arg.Status.Valid && !validKnowledgeLifecycleStatus(arg.Status.String) {
		return nil, validationError("invalid lifecycle_status")
	}
	if arg.SourceType.Valid && !validKnowledgeSourceType(arg.SourceType.String) {
		return nil, validationError("invalid source_type")
	}
	return s.Queries.ListKnowledgeItems(ctx, arg)
}

func (s *KnowledgeService) GetDetail(ctx context.Context, workspaceID, itemID pgtype.UUID) (KnowledgeDetail, error) {
	item, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeDetail{}, ErrKnowledgeNotFound
		}
		return KnowledgeDetail{}, err
	}
	sources, err := s.Queries.ListKnowledgeSources(ctx, db.ListKnowledgeSourcesParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	embeddings, err := s.Queries.ListKnowledgeEmbeddingMetadata(ctx, db.ListKnowledgeEmbeddingMetadataParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	var embeddingStatus *db.KnowledgeEmbeddingAttempt
	attempt, err := s.Queries.GetKnowledgeEmbeddingAttempt(ctx, db.GetKnowledgeEmbeddingAttemptParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeDetail{}, err
		}
	} else {
		embeddingStatus = &attempt
	}
	feedback, err := s.Queries.GetKnowledgeFeedbackSummary(ctx, db.GetKnowledgeFeedbackSummaryParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	targets, err := s.Queries.ListKnowledgePublishTargets(ctx, db.ListKnowledgePublishTargetsParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	return KnowledgeDetail{Item: item, Sources: sources, SourceSummary: summarizeKnowledgeSources(sources), PublishTargets: targets, Embeddings: embeddings, EmbeddingStatus: embeddingStatus, FeedbackSummary: feedback}, nil
}

func (s *KnowledgeService) GetSourceDetails(ctx context.Context, workspaceID, itemID pgtype.UUID) ([]KnowledgeSourceDetail, error) {
	if _, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: itemID, WorkspaceID: workspaceID}); err != nil {
		return nil, knowledgeItemLookupErr(err)
	}
	sources, err := s.Queries.ListKnowledgeSources(ctx, db.ListKnowledgeSourcesParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	details := make([]KnowledgeSourceDetail, 0, len(sources))
	for _, source := range sources {
		detail := KnowledgeSourceDetail{
			Source:        source,
			ResolvedTitle: source.SourceTitle,
			ResolvedURL:   source.SourceUrl,
			ResolvedNote:  source.SourceExcerpt,
		}
		if source.SourceID.Valid {
			switch source.SourceType {
			case "knowledge":
				item, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: source.SourceID, WorkspaceID: workspaceID})
				if err == nil {
					detail.ResolvedTitle = pgtype.Text{String: item.Title, Valid: true}
					detail.ResolvedURL = pgtype.Text{String: "/knowledge/" + util.UUIDToString(item.ID), Valid: true}
					detail.ResolvedNote = pgtype.Text{String: truncateKnowledgeText(item.ProblemPattern, 240), Valid: true}
				}
			case "issue":
				issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: source.SourceID, WorkspaceID: workspaceID})
				if err == nil {
					detail.ResolvedTitle = pgtype.Text{String: issue.Title, Valid: true}
					detail.ResolvedURL = pgtype.Text{String: fmt.Sprintf("/issues/%d", issue.Number), Valid: true}
				}
			case "comment":
				comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: source.SourceID, WorkspaceID: workspaceID})
				if err == nil {
					detail.ResolvedTitle = pgtype.Text{String: "Comment " + util.UUIDToString(comment.ID), Valid: true}
					detail.ResolvedURL = pgtype.Text{String: "/issues/" + util.UUIDToString(comment.IssueID) + "?comment=" + util.UUIDToString(comment.ID), Valid: true}
					detail.ResolvedNote = pgtype.Text{String: truncateKnowledgeText(comment.Content, 240), Valid: true}
				}
			case "agent_task":
				task, err := s.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: source.SourceID, WorkspaceID: workspaceID})
				if err == nil {
					detail.ResolvedTitle = pgtype.Text{String: "Agent task " + util.UUIDToString(task.ID), Valid: true}
					detail.ResolvedURL = pgtype.Text{String: "/issues/" + util.UUIDToString(task.IssueID) + "/tasks/" + util.UUIDToString(task.ID), Valid: true}
					detail.ResolvedNote = pgtype.Text{String: "status: " + task.Status, Valid: true}
				}
			}
		}
		details = append(details, detail)
	}
	return details, nil
}

func (s *KnowledgeService) Create(ctx context.Context, p KnowledgeCreateParams) (KnowledgeDetail, error) {
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		return KnowledgeDetail{}, validationError("title is required")
	}
	if p.Type == "" {
		p.Type = "lesson"
	}
	if p.ConfidenceStatus == "" {
		p.ConfidenceStatus = "medium"
	}
	if p.LifecycleStatus == "" {
		p.LifecycleStatus = "draft"
	}
	if err := validateKnowledgeEnums(p.Type, p.ConfidenceStatus, p.LifecycleStatus); err != nil {
		return KnowledgeDetail{}, err
	}
	if len(p.Sources) == 0 {
		return KnowledgeDetail{}, validationError("at least one source is required")
	}
	if err := validateOptionalKnowledgeFilters(ctx, s.Queries, p.WorkspaceID, p.ProjectID, p.AgentID); err != nil {
		return KnowledgeDetail{}, err
	}
	for _, source := range p.Sources {
		if err := validateKnowledgeSource(ctx, s.Queries, p.WorkspaceID, source); err != nil {
			return KnowledgeDetail{}, err
		}
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	item, err := qtx.CreateKnowledgeItem(ctx, db.CreateKnowledgeItemParams{
		WorkspaceID:         p.WorkspaceID,
		ProjectID:           p.ProjectID,
		AgentID:             p.AgentID,
		Title:               p.Title,
		Type:                p.Type,
		DomainLabels:        normalizeLabels(p.DomainLabels),
		ProblemPattern:      p.ProblemPattern,
		TriggerConditions:   p.TriggerConditions,
		DiagnosticSteps:     p.DiagnosticSteps,
		RecommendedPractice: p.RecommendedPractice,
		AntiPatterns:        p.AntiPatterns,
		Applicability:       p.Applicability,
		ConfidenceStatus:    p.ConfidenceStatus,
		LifecycleStatus:     p.LifecycleStatus,
		CreatedBy:           p.CreatedBy,
	})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	sources := make([]db.KnowledgeSource, 0, len(p.Sources))
	for _, source := range p.Sources {
		created, err := qtx.CreateKnowledgeSource(ctx, db.CreateKnowledgeSourceParams{
			KnowledgeItemID: item.ID,
			WorkspaceID:     p.WorkspaceID,
			SourceType:      source.SourceType,
			SourceID:        source.SourceID,
			SourceUrl:       source.SourceURL,
			SourceTitle:     source.SourceTitle,
			SourceExcerpt:   source.SourceExcerpt,
		})
		if err != nil {
			return KnowledgeDetail{}, err
		}
		sources = append(sources, created)
	}
	if err := tx.Commit(ctx); err != nil {
		return KnowledgeDetail{}, err
	}
	return KnowledgeDetail{Item: item, Sources: sources}, nil
}

func (s *KnowledgeService) Update(ctx context.Context, p KnowledgeUpdateParams) (db.KnowledgeItem, error) {
	if p.Title.Valid && strings.TrimSpace(p.Title.String) == "" {
		return db.KnowledgeItem{}, validationError("title is required")
	}
	if p.Title.Valid {
		p.Title.String = strings.TrimSpace(p.Title.String)
	}
	if p.Type.Valid && !validKnowledgeType(p.Type.String) {
		return db.KnowledgeItem{}, validationError("invalid type")
	}
	if p.ConfidenceStatus.Valid && !validKnowledgeConfidenceStatus(p.ConfidenceStatus.String) {
		return db.KnowledgeItem{}, validationError("invalid confidence_status")
	}
	if p.LifecycleStatus.Valid {
		return db.KnowledgeItem{}, validationError("use lifecycle action endpoints")
	}
	if err := validateOptionalKnowledgeFilters(ctx, s.Queries, p.WorkspaceID, p.ProjectID, p.AgentID); err != nil {
		return db.KnowledgeItem{}, err
	}
	labels := []string(nil)
	if p.DomainLabelsSet {
		labels = normalizeLabels(p.DomainLabels)
	}
	item, err := s.Queries.UpdateKnowledgeItem(ctx, db.UpdateKnowledgeItemParams{
		ProjectID:           p.ProjectID,
		AgentID:             p.AgentID,
		Title:               p.Title,
		Type:                p.Type,
		DomainLabels:        labels,
		ProblemPattern:      p.ProblemPattern,
		TriggerConditions:   p.TriggerConditions,
		DiagnosticSteps:     p.DiagnosticSteps,
		RecommendedPractice: p.RecommendedPractice,
		AntiPatterns:        p.AntiPatterns,
		Applicability:       p.Applicability,
		ConfidenceStatus:    p.ConfidenceStatus,
		UpdatedBy:           p.UpdatedBy,
		ID:                  p.ID,
		WorkspaceID:         p.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.KnowledgeItem{}, ErrKnowledgeNotFound
		}
		return db.KnowledgeItem{}, err
	}
	return item, nil
}

func (s *KnowledgeService) Review(ctx context.Context, workspaceID, itemID, actorID pgtype.UUID) (db.KnowledgeItem, error) {
	return s.setLifecycleStatus(ctx, s.Queries, workspaceID, itemID, actorID, "reviewed")
}

func (s *KnowledgeService) Publish(ctx context.Context, workspaceID, itemID, actorID pgtype.UUID) (KnowledgeDetail, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	if _, err := s.setLifecycleStatus(ctx, qtx, workspaceID, itemID, actorID, "published"); err != nil {
		return KnowledgeDetail{}, err
	}
	if _, err := qtx.UpsertKnowledgePublishTarget(ctx, db.UpsertKnowledgePublishTargetParams{
		KnowledgeItemID: itemID,
		WorkspaceID:     workspaceID,
		TargetType:      "rag",
		TargetTitle:     pgtype.Text{String: "Default RAG retrieval", Valid: true},
		CreatedBy:       actorID,
	}); err != nil {
		return KnowledgeDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return KnowledgeDetail{}, err
	}
	return s.GetDetail(ctx, workspaceID, itemID)
}

func (s *KnowledgeService) Archive(ctx context.Context, workspaceID, itemID, actorID pgtype.UUID) (db.KnowledgeItem, error) {
	return s.setLifecycleStatus(ctx, s.Queries, workspaceID, itemID, actorID, "archived")
}

func (s *KnowledgeService) Deprecate(ctx context.Context, workspaceID, itemID, actorID pgtype.UUID) (db.KnowledgeItem, error) {
	return s.setLifecycleStatus(ctx, s.Queries, workspaceID, itemID, actorID, "deprecated")
}

func (s *KnowledgeService) Restore(ctx context.Context, workspaceID, itemID, actorID pgtype.UUID) (db.KnowledgeItem, error) {
	item, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return db.KnowledgeItem{}, knowledgeItemLookupErr(err)
	}
	if item.LifecycleStatus != "archived" && item.LifecycleStatus != "deprecated" {
		return db.KnowledgeItem{}, validationError("only archived or deprecated knowledge can be restored")
	}
	targetStatus := "draft"
	if item.ReviewedBy.Valid {
		targetStatus = "reviewed"
	}
	return s.setLifecycleStatus(ctx, s.Queries, workspaceID, itemID, actorID, targetStatus)
}

func (s *KnowledgeService) DismissGovernance(ctx context.Context, workspaceID, itemID, actorID pgtype.UUID) (db.KnowledgeItem, error) {
	item, err := s.Queries.DismissKnowledgeGovernance(ctx, db.DismissKnowledgeGovernanceParams{
		ID:          itemID,
		WorkspaceID: workspaceID,
		UpdatedBy:   actorID,
	})
	if err != nil {
		return db.KnowledgeItem{}, knowledgeItemLookupErr(err)
	}
	if _, err := s.Queries.DismissKnowledgeGovernanceFindingsForItem(ctx, db.DismissKnowledgeGovernanceFindingsForItemParams{
		WorkspaceID:     workspaceID,
		KnowledgeItemID: itemID,
		ActorID:         actorID,
	}); err != nil {
		return db.KnowledgeItem{}, err
	}
	return item, nil
}

func (s *KnowledgeService) PublishToWiki(ctx context.Context, p KnowledgePublishWikiParams) (KnowledgeDetail, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	item, err := s.setLifecycleStatus(ctx, qtx, p.WorkspaceID, p.ItemID, p.ActorID, "published")
	if err != nil {
		return KnowledgeDetail{}, err
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = item.Title
	}
	content := p.Content
	if strings.TrimSpace(content) == "" {
		content = knowledgeWikiContent(item)
	}
	if p.ParentID.Valid {
		parent, err := qtx.GetWikiPage(ctx, db.GetWikiPageParams{ID: p.ParentID, WorkspaceID: p.WorkspaceID})
		if err != nil {
			return KnowledgeDetail{}, validationError("parent wiki page not found")
		}
		if parent.Type != "folder" {
			return KnowledgeDetail{}, validationError("parent must be a folder")
		}
	}
	pageID := p.WikiPageID
	if !pageID.Valid {
		if existing, err := qtx.GetKnowledgePublishTargetByType(ctx, db.GetKnowledgePublishTargetByTypeParams{
			KnowledgeItemID: p.ItemID,
			WorkspaceID:     p.WorkspaceID,
			TargetType:      "wiki",
		}); err == nil && existing.TargetID.Valid {
			pageID = existing.TargetID
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeDetail{}, err
		}
	}
	var page db.WikiPage
	if pageID.Valid {
		page, err = qtx.UpdateWikiPage(ctx, db.UpdateWikiPageParams{
			ID:          pageID,
			WorkspaceID: p.WorkspaceID,
			Title:       pgtype.Text{String: title, Valid: true},
			Content:     pgtype.Text{String: content, Valid: true},
			ParentID:    p.ParentID,
			UpdatedBy:   p.ActorUserID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				pageID = pgtype.UUID{} // target was deleted, create a new one
			} else {
				return KnowledgeDetail{}, knowledgePublishTargetErr(err, "wiki page not found")
			}
		}
	}
	if !pageID.Valid {
		position, err := qtx.GetMaxWikiPagePosition(ctx, db.GetMaxWikiPagePositionParams{WorkspaceID: p.WorkspaceID, ParentID: p.ParentID})
		if err != nil {
			return KnowledgeDetail{}, err
		}
		baseSlug := knowledgeSlugFromTitle(title)
		for attempt := 1; attempt <= 20; attempt++ {
			page, err = qtx.CreateWikiPage(ctx, db.CreateWikiPageParams{
				WorkspaceID: p.WorkspaceID,
				ParentID:    p.ParentID,
				Title:       title,
				Slug:        knowledgeSlugWithSuffix(baseSlug, attempt),
				Content:     pgtype.Text{String: content, Valid: true},
				Type:        pgtype.Text{String: "page", Valid: true},
				Position:    position + 1,
				CreatedBy:   p.ActorUserID,
				UpdatedBy:   p.ActorUserID,
			})
			if err == nil {
				break
			}
			if !isPgUniqueViolation(err) {
				return KnowledgeDetail{}, err
			}
		}
		if err != nil {
			return KnowledgeDetail{}, validationError("wiki page slug already exists")
		}
	}
	if _, err := qtx.UpsertKnowledgePublishTarget(ctx, db.UpsertKnowledgePublishTargetParams{
		KnowledgeItemID: p.ItemID,
		WorkspaceID:     p.WorkspaceID,
		TargetType:      "wiki",
		TargetID:        page.ID,
		TargetUrl:       pgtype.Text{String: "/wiki/" + util.UUIDToString(page.ID), Valid: true},
		TargetTitle:     pgtype.Text{String: page.Title, Valid: true},
		CreatedBy:       p.ActorID,
	}); err != nil {
		return KnowledgeDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return KnowledgeDetail{}, err
	}
	return s.GetDetail(ctx, p.WorkspaceID, p.ItemID)
}

func (s *KnowledgeService) PublishToSkill(ctx context.Context, p KnowledgePublishSkillParams) (KnowledgeDetail, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	item, err := s.setLifecycleStatus(ctx, qtx, p.WorkspaceID, p.ItemID, p.ActorID, "published")
	if err != nil {
		return KnowledgeDetail{}, err
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = knowledgeSlugFromTitle(item.Title)
	}
	description := strings.TrimSpace(p.Description)
	if description == "" {
		description = strings.TrimSpace(item.ProblemPattern)
		if description == "" {
			description = "Knowledge published from " + item.Title
		}
	}
	content := p.Content
	if strings.TrimSpace(content) == "" {
		content = knowledgeSkillContent(item, name, description)
	}
	skillID := p.SkillID
	if !skillID.Valid {
		if existing, err := qtx.GetKnowledgePublishTargetByType(ctx, db.GetKnowledgePublishTargetByTypeParams{
			KnowledgeItemID: p.ItemID,
			WorkspaceID:     p.WorkspaceID,
			TargetType:      "skill",
		}); err == nil && existing.TargetID.Valid {
			skillID = existing.TargetID
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeDetail{}, err
		}
	}
	if !skillID.Valid {
		if existing, err := qtx.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{WorkspaceID: p.WorkspaceID, Name: name}); err == nil {
			skillID = existing.ID
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeDetail{}, err
		}
	}
	config := []byte("{}")
	var skill db.Skill
	if skillID.Valid {
		skill, err = qtx.UpdateSkill(ctx, db.UpdateSkillParams{
			ID:          skillID,
			Name:        pgtype.Text{String: name, Valid: true},
			Description: pgtype.Text{String: description, Valid: true},
			Content:     pgtype.Text{String: content, Valid: true},
			Config:      config,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				skillID = pgtype.UUID{} // target was deleted, create a new one
			} else {
				return KnowledgeDetail{}, knowledgePublishTargetErr(err, "skill not found")
			}
		} else {
			if err := qtx.DeleteSkillFilesBySkill(ctx, skill.ID); err != nil {
				return KnowledgeDetail{}, err
			}
		}
	}
	if !skillID.Valid {
		skill, err = qtx.CreateSkill(ctx, db.CreateSkillParams{
			WorkspaceID: p.WorkspaceID,
			Name:        name,
			Description: description,
			Content:     content,
			Config:      config,
			CreatedBy:   p.ActorUserID,
		})
		if err != nil {
			if isPgUniqueViolation(err) {
				return KnowledgeDetail{}, validationError("skill name already exists")
			}
			return KnowledgeDetail{}, err
		}
	}
	files := append([]KnowledgeSkillFileInput{}, p.SupportingFiles...)
	if p.IncludeSourceMap {
		files = append(files, KnowledgeSkillFileInput{
			Path:    "references/source-map.md",
			Content: knowledgeSourceMapContent(item, p.ItemID),
		})
	}
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		if path == "" || skillpkg.IsReservedContentPath(path) {
			continue
		}
		if _, err := qtx.UpsertSkillFile(ctx, db.UpsertSkillFileParams{
			SkillID: skill.ID,
			Path:    path,
			Content: file.Content,
		}); err != nil {
			return KnowledgeDetail{}, err
		}
	}
	if _, err := qtx.UpsertKnowledgePublishTarget(ctx, db.UpsertKnowledgePublishTargetParams{
		KnowledgeItemID: p.ItemID,
		WorkspaceID:     p.WorkspaceID,
		TargetType:      "skill",
		TargetID:        skill.ID,
		TargetUrl:       pgtype.Text{String: "/skills/" + util.UUIDToString(skill.ID), Valid: true},
		TargetTitle:     pgtype.Text{String: skill.Name, Valid: true},
		CreatedBy:       p.ActorID,
	}); err != nil {
		return KnowledgeDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return KnowledgeDetail{}, err
	}
	return s.GetDetail(ctx, p.WorkspaceID, p.ItemID)
}

func (s *KnowledgeService) UpsertEmbedding(ctx context.Context, itemID, workspaceID pgtype.UUID, provider, model, contentHash string, embedding []float32) (db.UpsertKnowledgeEmbeddingRow, error) {
	dimension := len(embedding)
	if !validKnowledgeEmbeddingDimension(dimension) {
		return db.UpsertKnowledgeEmbeddingRow{}, validationError(fmt.Sprintf("embedding must have one of these dimensions: %v", SupportedKnowledgeEmbeddingDimensions))
	}
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(model) == "" || strings.TrimSpace(contentHash) == "" {
		return db.UpsertKnowledgeEmbeddingRow{}, validationError("provider, model, and content_hash are required")
	}
	if _, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: itemID, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.UpsertKnowledgeEmbeddingRow{}, ErrKnowledgeNotFound
		}
		return db.UpsertKnowledgeEmbeddingRow{}, err
	}
	params := db.UpsertKnowledgeEmbeddingParams{
		KnowledgeItemID: itemID,
		WorkspaceID:     workspaceID,
		Provider:        strings.TrimSpace(provider),
		Model:           strings.TrimSpace(model),
		ContentHash:     strings.TrimSpace(contentHash),
		Embedding:       pgvector.NewVector(embedding),
	}
	switch dimension {
	case 1536:
		return s.Queries.UpsertKnowledgeEmbedding(ctx, params)
	case 3072:
		row, err := s.Queries.UpsertKnowledgeEmbedding3072(ctx, db.UpsertKnowledgeEmbedding3072Params(params))
		return upsertKnowledgeEmbeddingRowFrom3072(row), err
	case 1024:
		row, err := s.Queries.UpsertKnowledgeEmbedding1024(ctx, db.UpsertKnowledgeEmbedding1024Params(params))
		return upsertKnowledgeEmbeddingRowFrom1024(row), err
	case 768:
		row, err := s.Queries.UpsertKnowledgeEmbedding768(ctx, db.UpsertKnowledgeEmbedding768Params(params))
		return upsertKnowledgeEmbeddingRowFrom768(row), err
	default:
		return db.UpsertKnowledgeEmbeddingRow{}, validationError(fmt.Sprintf("embedding must have one of these dimensions: %v", SupportedKnowledgeEmbeddingDimensions))
	}
}

func (s *KnowledgeService) Search(ctx context.Context, p KnowledgeSearchParams) ([]KnowledgeSearchResult, error) {
	results, _, err := s.searchWithRetrieval(ctx, p)
	return results, err
}

func (s *KnowledgeService) SearchForTaskClaim(ctx context.Context, p KnowledgeTaskClaimParams) ([]KnowledgeContextItem, error) {
	query := buildTaskClaimKnowledgeQuery(p)
	if query == "" {
		return nil, nil
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 8 {
		limit = 8
	}
	results, retrieval, err := s.searchWithRetrieval(ctx, KnowledgeSearchParams{
		WorkspaceID: p.WorkspaceID,
		MemberID:    p.MemberID,
		AgentTaskID: p.AgentTaskID,
		Query:       query,
		QuerySource: "task_claim",
		Limit:       limit,
		Filters: KnowledgeSearchFilters{
			Types: p.TypeFilters,
		},
	})
	if err != nil {
		return nil, err
	}
	out := make([]KnowledgeContextItem, 0, len(results))
	for idx, result := range results {
		if !eligibleForTaskClaimKnowledge(result.Item, p.ConfidenceThreshold) {
			continue
		}
		item := KnowledgeContextItem{
			ID:                util.UUIDToString(result.Item.ID),
			Title:             result.Item.Title,
			Summary:           truncateKnowledgeText(firstNonEmpty(result.Item.ProblemPattern, result.Item.Applicability, result.Item.TriggerConditions), 500),
			RecommendedAction: truncateKnowledgeText(result.Item.RecommendedPractice, 700),
			AntiPatterns:      truncateKnowledgeText(result.Item.AntiPatterns, 400),
			SourceIssue:       s.sourceIssueIdentifier(ctx, p.WorkspaceID, result.Item.ID),
			Score:             result.FinalScore,
			Reason:            taskClaimInjectionReason(result),
		}
		out = append(out, item)
		if _, err := s.Queries.CreateKnowledgeInjectionEvent(ctx, db.CreateKnowledgeInjectionEventParams{
			WorkspaceID:      p.WorkspaceID,
			KnowledgeItemID:  result.Item.ID,
			AgentTaskID:      p.AgentTaskID,
			InjectionTarget:  "daemon_brief",
			RetrievalEventID: retrieval.ID,
			Rank:             pgtype.Int4{Int32: int32(idx + 1), Valid: true},
			Score:            pgtype.Float8{Float64: result.FinalScore, Valid: true},
			InjectionReason:  pgtype.Text{String: item.Reason, Valid: item.Reason != ""},
			TokenBudget:      pgtype.Int4{Int32: p.TokenBudget, Valid: p.TokenBudget > 0},
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *KnowledgeService) searchWithRetrieval(ctx context.Context, p KnowledgeSearchParams) ([]KnowledgeSearchResult, db.KnowledgeRetrievalEvent, error) {
	query := buildInteractiveKnowledgeSearchQuery(p)
	if p.Limit <= 0 {
		p.Limit = 10
	}
	if p.Limit > 50 {
		p.Limit = 50
	}
	if query == "" && len(p.Embedding) == 0 {
		return nil, db.KnowledgeRetrievalEvent{}, validationError("query or embedding is required")
	}
	if len(p.Embedding) > 0 && !validKnowledgeEmbeddingDimension(len(p.Embedding)) {
		return nil, db.KnowledgeRetrievalEvent{}, validationError(fmt.Sprintf("embedding must have one of these dimensions: %v", SupportedKnowledgeEmbeddingDimensions))
	}
	if err := validateSearchFilters(ctx, s.Queries, p.WorkspaceID, p.Filters); err != nil {
		return nil, db.KnowledgeRetrievalEvent{}, err
	}
	if err := validateFilterEnums(p.Filters); err != nil {
		return nil, db.KnowledgeRetrievalEvent{}, err
	}

	resultMap := map[string]*KnowledgeSearchResult{}
	if query != "" {
		rows, err := s.Queries.SearchKnowledgeText(ctx, db.SearchKnowledgeTextParams{
			Limit:       p.Limit,
			Query:       query,
			WorkspaceID: p.WorkspaceID,
			Types:       p.Filters.Types,
			Statuses:    p.Filters.Statuses,
			ProjectID:   p.Filters.ProjectID,
			AgentID:     p.Filters.AgentID,
			Labels:      normalizeLabels(p.Filters.Labels),
		})
		if err != nil {
			return nil, db.KnowledgeRetrievalEvent{}, err
		}
		for _, row := range rows {
			key := util.UUIDToString(row.ID)
			item := knowledgeItemFromTextRow(row)
			resultMap[key] = &KnowledgeSearchResult{
				Item:        item,
				TextScore:   row.TextScore,
				FinalScore:  row.TextScore,
				MatchReason: "text",
			}
		}
	}
	if len(p.Embedding) > 0 {
		rows, err := s.searchKnowledgeVector(ctx, p)
		if err != nil {
			return nil, db.KnowledgeRetrievalEvent{}, err
		}
		for _, row := range rows {
			key := util.UUIDToString(row.Item.ID)
			result, ok := resultMap[key]
			if !ok {
				result = &KnowledgeSearchResult{Item: row.Item, MatchReason: "vector"}
				resultMap[key] = result
			} else {
				result.MatchReason = "hybrid"
			}
			result.VectorScore = row.VectorScore
			if query != "" {
				result.FinalScore = result.TextScore + row.VectorScore
			} else {
				result.FinalScore = row.VectorScore
			}
		}
	}

	results := make([]KnowledgeSearchResult, 0, len(resultMap))
	for _, result := range resultMap {
		result.FinalScore = applyKnowledgeGovernanceScore(result.Item, result.FinalScore)
		results = append(results, *result)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].FinalScore == results[j].FinalScore {
			return results[i].Item.UpdatedAt.Time.After(results[j].Item.UpdatedAt.Time)
		}
		return results[i].FinalScore > results[j].FinalScore
	})
	if int32(len(results)) > p.Limit {
		results = results[:p.Limit]
	}
	for i := range results {
		sources, err := s.Queries.ListKnowledgeSources(ctx, db.ListKnowledgeSourcesParams{
			KnowledgeItemID: results[i].Item.ID,
			WorkspaceID:     p.WorkspaceID,
		})
		if err != nil {
			return nil, db.KnowledgeRetrievalEvent{}, err
		}
		results[i].SourceSummary = summarizeKnowledgeSources(sources)
	}
	retrieval, err := s.recordRetrieval(ctx, p, query, results)
	if err != nil {
		return nil, db.KnowledgeRetrievalEvent{}, err
	}
	if p.QuerySource == "agent_search" && p.AgentTaskID.Valid {
		for _, result := range results {
			if _, err := s.RecordUsage(ctx, KnowledgeUsageParams{
				WorkspaceID:     p.WorkspaceID,
				KnowledgeItemID: result.Item.ID,
				AgentTaskID:     p.AgentTaskID,
				UsageSource:     "active_search",
				ReferenceText:   query,
			}); err != nil {
				slog.Warn("record knowledge active-search usage failed",
					"knowledge_item_id", util.UUIDToString(result.Item.ID),
					"agent_task_id", util.UUIDToString(p.AgentTaskID),
					"error", err,
				)
			}
		}
	}
	return results, retrieval, nil
}

type knowledgeVectorSearchRow struct {
	Item        db.KnowledgeItem
	VectorScore float64
}

func (s *KnowledgeService) searchKnowledgeVector(ctx context.Context, p KnowledgeSearchParams) ([]knowledgeVectorSearchRow, error) {
	params := db.SearchKnowledgeVectorParams{
		Embedding:   pgvector.NewVector(p.Embedding),
		WorkspaceID: p.WorkspaceID,
		Types:       p.Filters.Types,
		Statuses:    p.Filters.Statuses,
		ProjectID:   p.Filters.ProjectID,
		AgentID:     p.Filters.AgentID,
		Labels:      normalizeLabels(p.Filters.Labels),
		Limit:       p.Limit,
	}
	switch len(p.Embedding) {
	case 1536:
		rows, err := s.Queries.SearchKnowledgeVector(ctx, params)
		return knowledgeVectorRowsFrom1536(rows), err
	case 3072:
		rows, err := s.Queries.SearchKnowledgeVector3072(ctx, db.SearchKnowledgeVector3072Params(params))
		return knowledgeVectorRowsFrom3072(rows), err
	case 1024:
		rows, err := s.Queries.SearchKnowledgeVector1024(ctx, db.SearchKnowledgeVector1024Params(params))
		return knowledgeVectorRowsFrom1024(rows), err
	case 768:
		rows, err := s.Queries.SearchKnowledgeVector768(ctx, db.SearchKnowledgeVector768Params(params))
		return knowledgeVectorRowsFrom768(rows), err
	default:
		return nil, validationError(fmt.Sprintf("embedding must have one of these dimensions: %v", SupportedKnowledgeEmbeddingDimensions))
	}
}

func (s *KnowledgeService) AddFeedback(ctx context.Context, p KnowledgeFeedbackParams) (db.KnowledgeFeedback, error) {
	if !validKnowledgeFeedbackValue(p.Value) {
		return db.KnowledgeFeedback{}, validationError("invalid feedback value")
	}
	if _, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: p.KnowledgeItemID, WorkspaceID: p.WorkspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.KnowledgeFeedback{}, ErrKnowledgeNotFound
		}
		return db.KnowledgeFeedback{}, err
	}
	if p.AgentTaskID.Valid {
		if _, err := s.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: p.AgentTaskID, WorkspaceID: p.WorkspaceID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.KnowledgeFeedback{}, validationError("agent_task_id not found")
			}
			return db.KnowledgeFeedback{}, err
		}
	}
	return s.Queries.CreateKnowledgeFeedback(ctx, db.CreateKnowledgeFeedbackParams{
		KnowledgeItemID: p.KnowledgeItemID,
		WorkspaceID:     p.WorkspaceID,
		MemberID:        p.MemberID,
		AgentTaskID:     p.AgentTaskID,
		Value:           p.Value,
		Note:            p.Note,
	})
}

func (s *KnowledgeService) ListCandidates(ctx context.Context, arg db.ListKnowledgeCandidatesParams) ([]db.KnowledgeCandidate, error) {
	if arg.Limit <= 0 {
		arg.Limit = 50
	}
	if arg.Limit > 100 {
		arg.Limit = 100
	}
	if arg.Status.Valid && !validKnowledgeCandidateStatus(arg.Status.String) {
		return nil, validationError("invalid status")
	}
	if arg.SourceType.Valid && !validKnowledgeCandidateSourceType(arg.SourceType.String) {
		return nil, validationError("invalid source_type")
	}
	return s.Queries.ListKnowledgeCandidates(ctx, arg)
}

func (s *KnowledgeService) EvaluateCandidate(ctx context.Context, p KnowledgeCandidateEvaluateParams) (db.KnowledgeCandidate, error) {
	p.SourceType = strings.TrimSpace(p.SourceType)
	p.TriggerReason = strings.TrimSpace(p.TriggerReason)
	if p.Manual && p.TriggerReason == "" {
		p.TriggerReason = "manual"
	}
	if p.TriggerReason == "" {
		switch p.SourceType {
		case "issue":
			p.TriggerReason = "issue_done"
		case "agent_task":
			p.TriggerReason = "task_completed"
		case "comment":
			p.TriggerReason = "comment_signal"
		}
	}
	if !validKnowledgeCandidateSourceType(p.SourceType) {
		return db.KnowledgeCandidate{}, validationError("invalid source_type")
	}
	if !p.SourceID.Valid {
		return db.KnowledgeCandidate{}, validationError("source_id is required")
	}

	src, err := s.resolveCandidateSource(ctx, p)
	if err != nil {
		return db.KnowledgeCandidate{}, err
	}

	// Requirement 5: evaluate skip rules before scoring
	text := src.Issue.Title
	if src.Issue.Description.Valid {
		text += "\n" + src.Issue.Description.String
	}
	evidence := map[string]any{
		"skip_check": skipRuleCheck{Evaluated: true, MatchedRule: nil},
	}
	if skipReason, shouldSkip := evaluateSkipRules(text); shouldSkip {
		evidence["skip_check"] = skipRuleCheck{Evaluated: true, MatchedRule: skipReason}
		evidenceJSON, _ := json.Marshal(evidence)
		return s.Queries.UpsertKnowledgeCandidate(ctx, db.UpsertKnowledgeCandidateParams{
			WorkspaceID:    p.WorkspaceID,
			IssueID:        src.Issue.ID,
			CommentID:      src.CommentID,
			AgentTaskID:    src.AgentTaskID,
			SourceType:     p.SourceType,
			SourceID:       p.SourceID,
			TriggerReason:  p.TriggerReason,
			SignalStrength: "none",
			Signals:        []string{"skip:" + skipReason},
			Score:          0,
			Status:         "rejected",
			DedupeKey:      knowledgeCandidateDedupeKey(p.SourceType, p.SourceID, p.TriggerReason),
			CreatedBy:      p.CreatedBy,
			Metadata:       []byte("{}"),
			Evidence:       evidenceJSON,
		})
	}

	signals, score, strength, status, evidenceUpdates := s.scoreCandidate(ctx, p, src, text)
	for k, v := range evidenceUpdates {
		evidence[k] = v
	}

	meta := map[string]any{
		"manual":       p.Manual,
		"source_type":  p.SourceType,
		"source_id":    util.UUIDToString(p.SourceID),
		"issue_id":     util.UUIDToString(src.Issue.ID),
		"trigger":      p.TriggerReason,
		"signal_count": len(signals),
	}
	for k, v := range p.AdditionalMeta {
		meta[k] = v
	}
	metadata, err := json.Marshal(meta)
	if err != nil {
		return db.KnowledgeCandidate{}, err
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return db.KnowledgeCandidate{}, err
	}

	return s.Queries.UpsertKnowledgeCandidate(ctx, db.UpsertKnowledgeCandidateParams{
		WorkspaceID:    p.WorkspaceID,
		IssueID:        src.Issue.ID,
		CommentID:      src.CommentID,
		AgentTaskID:    src.AgentTaskID,
		SourceType:     p.SourceType,
		SourceID:       p.SourceID,
		TriggerReason:  p.TriggerReason,
		SignalStrength: strength,
		Signals:        signals,
		Score:          score,
		Status:         status,
		DedupeKey:      knowledgeCandidateDedupeKey(p.SourceType, p.SourceID, p.TriggerReason),
		CreatedBy:      p.CreatedBy,
		Metadata:       metadata,
		Evidence:       evidenceJSON,
	})
}

func (s *KnowledgeService) EvaluateIssueDoneCandidate(ctx context.Context, issue db.Issue) (db.KnowledgeCandidate, error) {
	if issue.Status != "done" {
		return db.KnowledgeCandidate{}, validationError("issue is not done")
	}
	prs, err := s.Queries.ListPullRequestsByIssue(ctx, issue.ID)
	if err != nil {
		prs = nil
	}
	prEvidence := make([]prEvidenceItem, 0, len(prs))
	for _, pr := range prs {
		item := prEvidenceItem{
			Number:       pr.PrNumber,
			Title:        pr.Title,
			RepoOwner:    pr.RepoOwner,
			RepoName:     pr.RepoName,
			State:        pr.State,
			Additions:    pr.Additions,
			Deletions:    pr.Deletions,
			ChangedFiles: pr.ChangedFiles,
		}
		if pr.MergedAt.Valid {
			item.MergedAt = pr.MergedAt.Time.Format(time.RFC3339)
		}
		prEvidence = append(prEvidence, item)
	}
	additionalMeta := map[string]any{
		"pr_count": len(prEvidence),
	}
	if len(prEvidence) > 0 {
		additionalMeta["pr_data"] = prEvidence
	}
	return s.EvaluateCandidate(ctx, KnowledgeCandidateEvaluateParams{
		WorkspaceID:    issue.WorkspaceID,
		SourceType:     "issue",
		SourceID:       issue.ID,
		TriggerReason:  "issue_done",
		Issue:          &issue,
		AdditionalMeta: additionalMeta,
	})
}

func (s *KnowledgeService) EvaluateTaskCompletedCandidate(ctx context.Context, task db.AgentTaskQueue, result []byte) (db.KnowledgeCandidate, error) {
	if task.Status != "completed" || !task.IssueID.Valid {
		return db.KnowledgeCandidate{}, validationError("task is not a completed issue task")
	}
	workspaceID, err := s.taskWorkspaceID(ctx, task)
	if err != nil {
		return db.KnowledgeCandidate{}, err
	}
	return s.EvaluateCandidate(ctx, KnowledgeCandidateEvaluateParams{
		WorkspaceID:   workspaceID,
		SourceType:    "agent_task",
		SourceID:      task.ID,
		TriggerReason: "task_completed",
		AgentTask:     &task,
		TaskResult:    result,
	})
}

func (s *KnowledgeService) resolveCandidateSource(ctx context.Context, p KnowledgeCandidateEvaluateParams) (candidateSource, error) {
	switch p.SourceType {
	case "issue":
		issue := db.Issue{}
		if p.Issue != nil {
			issue = *p.Issue
		} else {
			var err error
			issue, err = s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: p.SourceID, WorkspaceID: p.WorkspaceID})
			if err != nil {
				return candidateSource{}, sourceLookupErr(err)
			}
		}
		return candidateSource{Issue: issue}, nil
	case "comment":
		comment := db.Comment{}
		if p.Comment != nil {
			comment = *p.Comment
		} else {
			var err error
			comment, err = s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: p.SourceID, WorkspaceID: p.WorkspaceID})
			if err != nil {
				return candidateSource{}, sourceLookupErr(err)
			}
		}
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: comment.IssueID, WorkspaceID: p.WorkspaceID})
		if err != nil {
			return candidateSource{}, sourceLookupErr(err)
		}
		return candidateSource{Issue: issue, CommentID: comment.ID}, nil
	case "agent_task":
		task := db.AgentTaskQueue{}
		if p.AgentTask != nil {
			task = *p.AgentTask
		} else {
			var err error
			task, err = s.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: p.SourceID, WorkspaceID: p.WorkspaceID})
			if err != nil {
				return candidateSource{}, sourceLookupErr(err)
			}
		}
		if !task.IssueID.Valid {
			return candidateSource{}, validationError("agent_task source must be linked to an issue")
		}
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: task.IssueID, WorkspaceID: p.WorkspaceID})
		if err != nil {
			return candidateSource{}, sourceLookupErr(err)
		}
		return candidateSource{Issue: issue, CommentID: task.TriggerCommentID, AgentTaskID: task.ID}, nil
	default:
		return candidateSource{}, validationError("invalid source_type")
	}
}

func (s *KnowledgeService) scoreCandidate(ctx context.Context, p KnowledgeCandidateEvaluateParams, src candidateSource, text string) ([]string, int32, string, string, map[string]any) {
	signals := []string{}
	score := int32(0)
	evidenceUpdates := map[string]any{}
	if p.Manual || p.TriggerReason == "manual" {
		return []string{"manual_mark"}, 100, "manual", "accepted", evidenceUpdates
	}

	if p.AgentTask != nil {
		text += "\n" + extractTaskOutput(p.TaskResult, p.AgentTask.Result)
		if p.AgentTask.ParentTaskID.Valid || p.AgentTask.Attempt > 1 {
			signals = append(signals, "retry_success")
			score += 75
			retryEvidence := s.buildRetryEvidence(ctx, p.WorkspaceID, p.AgentTask)
			evidenceUpdates["retry_chain"] = retryEvidence
			if retryEvidence.HasClearError {
				signals = append(signals, "retry_with_clear_error")
				score += 10
			}
			if retryEvidence.TotalAttempts >= 3 {
				signals = append(signals, "multi_retry")
				score += 10
			}
		}
		if p.AgentTask.TriggerCommentID.Valid {
			signals = append(signals, "follow_up_task_success")
			score += 45
			if comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: p.AgentTask.TriggerCommentID, WorkspaceID: p.WorkspaceID}); err == nil {
				text += "\n" + comment.Content
				if looksLikeUserCorrection(comment.Content) {
					signals = append(signals, "user_correction")
					score += 45
				}
			}
		}
		if p.AgentTask.StartedAt.Valid && p.AgentTask.CompletedAt.Valid && p.AgentTask.CompletedAt.Time.Sub(p.AgentTask.StartedAt.Time) >= 15*time.Minute {
			signals = append(signals, "long_running_task")
			score += 15
		}
	}

	if p.SourceType == "issue" {
		outcomes, err := s.Queries.CountIssueTaskOutcomesForKnowledgeCandidate(ctx, src.Issue.ID)
		if err == nil {
			if outcomes.TaskCount == 0 {
				return []string{"no_agent_task"}, 0, "none", "rejected", evidenceUpdates
			}
			if outcomes.FailedCount > 0 && outcomes.CompletedCount > 0 {
				signals = append(signals, "failed_then_completed")
				score += 75
			}
			if outcomes.CommentTriggeredCount > 0 {
				signals = append(signals, "comment_triggered_task")
				score += 25
			}
			if outcomes.MaxAttempt > 1 {
				signals = append(signals, "retry_success")
				score += 45
			}
		}
		comments, err := s.Queries.ListIssueCommentsForKnowledgeCandidate(ctx, db.ListIssueCommentsForKnowledgeCandidateParams{
			WorkspaceID: p.WorkspaceID,
			IssueID:     src.Issue.ID,
			Limit:       100,
		})
		if err == nil {
			var correctionCount int32
			for _, comment := range comments {
				text += "\n" + comment.Content
				if comment.AuthorType == "member" && looksLikeUserCorrection(comment.Content) {
					correctionCount++
					if correctionCount == 1 {
						signals = append(signals, "user_correction")
						score += 35
					}
				}
			}
			if correctionCount >= 2 {
				signals = append(signals, "multi_round_correction")
				score += 10
			}
			if correctionCount > 0 {
				correctionRounds := buildCorrectionEvidence(comments)
				evidenceUpdates["correction_rounds"] = correctionRounds
			}
		}

		// Requirement 3: PR evidence signal
		prEvidence := buildPREvidenceFromMeta(p.AdditionalMeta)
		if len(prEvidence) > 0 {
			signals = append(signals, "pr_merged")
			score += 30
			evidenceUpdates["pr_evidence"] = prEvidence
			for _, pr := range prEvidence {
				if pr.ChangedFiles >= 5 && (pr.Additions+pr.Deletions) >= 100 {
					signals = append(signals, "substantial_pr")
					score += 10
					break
				}
			}
		}
	}

	if looksReusableKnowledge(text) {
		signals = append(signals, "reusable_debug_context")
		score += 30
	}

	// Requirement 4: historical similarity signal
	simSignal, simScore, simMatches := s.scoreSimilarity(ctx, p.WorkspaceID, text)
	if simSignal == "near_duplicate" {
		evidenceUpdates["similarity"] = map[string]any{
			"top_matches":    simMatches,
			"max_similarity": simMatches[0].VectorScore,
		}
		signals = append(signals, "near_duplicate")
		return signals, 0, "none", "rejected", evidenceUpdates
	}
	if simSignal != "" {
		signals = append(signals, simSignal)
		score += simScore
	}
	if len(simMatches) > 0 {
		evidenceUpdates["similarity"] = map[string]any{
			"top_matches":    simMatches,
			"max_similarity": simMatches[0].VectorScore,
		}
	}

	signals = uniqueSignals(signals)
	if score > 100 {
		score = 100
	}
	if score >= 80 {
		return signals, score, "strong", "accepted", evidenceUpdates
	}
	if score >= 50 {
		return signals, score, "weak", "pending", evidenceUpdates
	}
	if len(signals) == 0 {
		signals = []string{"no_reusable_signal"}
	}
	return signals, score, "none", "rejected", evidenceUpdates
}

func (s *KnowledgeService) taskWorkspaceID(ctx context.Context, task db.AgentTaskQueue) (pgtype.UUID, error) {
	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return agent.WorkspaceID, nil
}

// ── Skip rules (Requirement 5) ────────────────────────────────────────────────

func evaluateSkipRules(text string) (string, bool) {
	lower := strings.ToLower(text)
	noReuseKeywords := []string{
		"bump version", "update dependencies", "update deps", "chore:", "ci:",
		"release:", "bump ", "deps:", "routine maintenance", "dependency update",
	}
	for _, kw := range noReuseKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return "no_reuse_value", true
		}
	}
	oneOffKeywords := []string{
		"one-time", "一次性", "临时", "adhoc", "one-off", "single use",
	}
	for _, kw := range oneOffKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return "one_off_business", true
		}
	}
	contentiousKeywords := []string{
		"revert", "rollback", "回滚", "撤销", "wrong approach", "方案不对",
		"still broken", "regression in",
	}
	for _, kw := range contentiousKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return "contentious", true
		}
	}
	securityKeywords := []string{
		"credential leak", "password leak", "secret key", "api key leak",
		"token leak", "exploit", "cve-", "security vulnerability", "unauthorized access",
	}
	for _, kw := range securityKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return "security_sensitive", true
		}
	}
	return "", false
}

// ── Retry evidence (Requirement 1) ────────────────────────────────────────────

func (s *KnowledgeService) buildRetryEvidence(ctx context.Context, workspaceID pgtype.UUID, task *db.AgentTaskQueue) retryChainEvidence {
	chain := retryChainEvidence{TotalAttempts: int(task.Attempt)}
	if !task.ParentTaskID.Valid {
		return chain
	}

	const maxHops = 10
	currentID := task.ParentTaskID
	visited := map[string]bool{}
	for hop := 0; hop < maxHops && currentID.Valid; hop++ {
		key := util.UUIDToString(currentID)
		if visited[key] {
			break
		}
		visited[key] = true

		parent, err := s.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{
			ID:          currentID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			break
		}
		if parent.Status == "failed" {
			errSummary := summarizeTaskError(parent)
			chain.Failures = append(chain.Failures, retryFailure{
				Attempt:      int(parent.Attempt),
				Status:       parent.Status,
				ErrorSummary: errSummary,
			})
			chain.TotalAttempts = int(parent.Attempt)
			if errSummary != "" {
				chain.HasClearError = true
			}
		}
		currentID = parent.ParentTaskID
	}

	chain.FinalSuccess = &retrySuccess{
		Attempt: int(task.Attempt),
		TaskID:  util.UUIDToString(task.ID),
		Status:  task.Status,
	}
	return chain
}

func summarizeTaskError(task db.AgentTaskQueue) string {
	if len(task.Result) == 0 {
		return ""
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(task.Result, &payload); err != nil {
		return string(task.Result)
	}
	if len(payload.Error) > 500 {
		return payload.Error[:500]
	}
	return payload.Error
}

// ── Correction evidence (Requirement 2) ───────────────────────────────────────

func buildCorrectionEvidence(comments []db.ListIssueCommentsForKnowledgeCandidateRow) []correctionRound {
	var rounds []correctionRound
	for _, c := range comments {
		if c.AuthorType == "member" && looksLikeUserCorrection(c.Content) {
			text := c.Content
			if len(text) > 200 {
				text = text[:200]
			}
			rounds = append(rounds, correctionRound{
				CommentID:   util.UUIDToString(c.ID),
				CommentText: text,
			})
		}
	}
	return rounds
}

// ── PR evidence (Requirement 3) ───────────────────────────────────────────────

func buildPREvidenceFromMeta(meta map[string]any) []prEvidenceItem {
	if meta == nil {
		return nil
	}
	raw, ok := meta["pr_data"]
	if !ok {
		return nil
	}
	items, ok := raw.([]prEvidenceItem)
	if ok {
		return items
	}
	// Handle the case where pr_data is JSON-decoded as []interface{}
	slice, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var result []prEvidenceItem
	for _, s := range slice {
		m, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		item := prEvidenceItem{}
		if v, ok := m["number"].(float64); ok {
			item.Number = int32(v)
		}
		if v, ok := m["title"].(string); ok {
			item.Title = v
		}
		if v, ok := m["repo_owner"].(string); ok {
			item.RepoOwner = v
		}
		if v, ok := m["repo_name"].(string); ok {
			item.RepoName = v
		}
		if v, ok := m["state"].(string); ok {
			item.State = v
		}
		if v, ok := m["merged_at"].(string); ok {
			item.MergedAt = v
		}
		if v, ok := m["additions"].(float64); ok {
			item.Additions = int32(v)
		}
		if v, ok := m["deletions"].(float64); ok {
			item.Deletions = int32(v)
		}
		if v, ok := m["changed_files"].(float64); ok {
			item.ChangedFiles = int32(v)
		}
		result = append(result, item)
	}
	return result
}

// ── Historical similarity (Requirement 4) ─────────────────────────────────────

func (s *KnowledgeService) scoreSimilarity(ctx context.Context, workspaceID pgtype.UUID, content string) (string, int32, []similarityMatch) {
	if s.Embedder == nil {
		return "", 0, nil
	}
	if len(content) > 8000 {
		content = content[:8000]
	}

	// Resolve workspace-aware engine so that local runtime mode
	// can block embedding instead of silently using the cloud provider.
	engine := s.Embedder
	if wsEngine, ok := s.Embedder.(workspaceCuratorEngine); ok {
		engine = wsEngine.ForWorkspace(ctx, workspaceID)
	}

	embedding, err := engine.BuildEmbedding(ctx, content)
	if err != nil {
		slog.Warn("similarity embedding failed", "error", err)
		return "", 0, nil
	}
	rows, err := s.searchKnowledgeVector(ctx, KnowledgeSearchParams{
		WorkspaceID: workspaceID,
		Embedding:   embedding,
		Limit:       5,
	})
	if err != nil {
		slog.Warn("similarity search failed", "error", err)
		return "", 0, nil
	}

	var maxSim float64
	matches := make([]similarityMatch, 0, len(rows))
	for _, row := range rows {
		if row.VectorScore > maxSim {
			maxSim = row.VectorScore
		}
		matches = append(matches, similarityMatch{
			KnowledgeItemID: util.UUIDToString(row.Item.ID),
			Title:           row.Item.Title,
			VectorScore:     row.VectorScore,
		})
	}

	if maxSim >= 0.95 {
		return "near_duplicate", 0, matches
	}
	if maxSim >= 0.85 {
		return "similarity_very_high", 25, matches
	}
	if maxSim >= 0.70 {
		return "similarity_high", 15, matches
	}
	if maxSim >= 0.50 {
		return "similarity_moderate", 10, matches
	}
	return "", 0, matches
}

// ── PR-merge candidate (Requirement 3) ────────────────────────────────────────

func (s *KnowledgeService) EvaluateIssuePRMergedCandidate(ctx context.Context, issue db.Issue) (db.KnowledgeCandidate, error) {
	if issue.Status != "done" {
		return db.KnowledgeCandidate{}, validationError("issue is not done")
	}
	prs, err := s.Queries.ListPullRequestsByIssue(ctx, issue.ID)
	if err != nil {
		slog.Warn("failed to fetch PRs for candidate", "issue_id", util.UUIDToString(issue.ID), "error", err)
		prs = nil
	}
	prEvidence := make([]prEvidenceItem, 0, len(prs))
	for _, pr := range prs {
		item := prEvidenceItem{
			Number:       pr.PrNumber,
			Title:        pr.Title,
			RepoOwner:    pr.RepoOwner,
			RepoName:     pr.RepoName,
			State:        pr.State,
			Additions:    pr.Additions,
			Deletions:    pr.Deletions,
			ChangedFiles: pr.ChangedFiles,
		}
		if pr.MergedAt.Valid {
			item.MergedAt = pr.MergedAt.Time.Format(time.RFC3339)
		}
		prEvidence = append(prEvidence, item)
	}
	return s.EvaluateCandidate(ctx, KnowledgeCandidateEvaluateParams{
		WorkspaceID:   issue.WorkspaceID,
		SourceType:    "issue",
		SourceID:      issue.ID,
		TriggerReason: "pr_merged",
		Issue:         &issue,
		AdditionalMeta: map[string]any{
			"pr_count": len(prEvidence),
			"pr_data":  prEvidence,
		},
	})
}

func (s *KnowledgeService) recordRetrieval(ctx context.Context, p KnowledgeSearchParams, query string, results []KnowledgeSearchResult) (db.KnowledgeRetrievalEvent, error) {
	mode := "text"
	if query == "" {
		mode = "vector"
	} else if len(p.Embedding) > 0 {
		mode = "hybrid"
	}
	topIDs := make([]pgtype.UUID, 0, len(results))
	resultScores := make([]map[string]any, 0, len(results))
	for idx, result := range results {
		topIDs = append(topIDs, result.Item.ID)
		resultScores = append(resultScores, map[string]any{
			"knowledge_item_id": util.UUIDToString(result.Item.ID),
			"rank":              idx + 1,
			"text_score":        result.TextScore,
			"vector_score":      result.VectorScore,
			"final_score":       result.FinalScore,
			"match_reason":      result.MatchReason,
		})
	}
	filters, err := json.Marshal(p.Filters)
	if err != nil {
		return db.KnowledgeRetrievalEvent{}, err
	}
	scores, err := json.Marshal(resultScores)
	if err != nil {
		return db.KnowledgeRetrievalEvent{}, err
	}
	var queryText pgtype.Text
	if query != "" {
		queryText = pgtype.Text{String: query, Valid: true}
	}
	querySource := strings.TrimSpace(p.QuerySource)
	if querySource == "" {
		querySource = "interactive"
	}
	return s.Queries.CreateKnowledgeRetrievalEvent(ctx, db.CreateKnowledgeRetrievalEventParams{
		WorkspaceID:         p.WorkspaceID,
		MemberID:            p.MemberID,
		AgentTaskID:         p.AgentTaskID,
		Query:               queryText,
		QuerySource:         querySource,
		RetrievalMode:       mode,
		Filters:             filters,
		ResultCount:         int32(len(results)),
		TopKnowledgeItemIds: topIDs,
		ResultScores:        scores,
	})
}

func (s *KnowledgeService) RecordUsage(ctx context.Context, p KnowledgeUsageParams) (db.KnowledgeUsageEvent, error) {
	if p.UsageSource != "agent_reference" && p.UsageSource != "active_search" {
		return db.KnowledgeUsageEvent{}, validationError("invalid usage_source")
	}
	if _, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: p.KnowledgeItemID, WorkspaceID: p.WorkspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.KnowledgeUsageEvent{}, ErrKnowledgeNotFound
		}
		return db.KnowledgeUsageEvent{}, err
	}
	return s.Queries.CreateKnowledgeUsageEvent(ctx, db.CreateKnowledgeUsageEventParams{
		WorkspaceID:     p.WorkspaceID,
		KnowledgeItemID: p.KnowledgeItemID,
		AgentTaskID:     p.AgentTaskID,
		UsageSource:     p.UsageSource,
		ReferenceText:   pgtype.Text{String: strings.TrimSpace(p.ReferenceText), Valid: strings.TrimSpace(p.ReferenceText) != ""},
		TaskStatus:      pgtype.Text{String: strings.TrimSpace(p.TaskStatus), Valid: strings.TrimSpace(p.TaskStatus) != ""},
		TaskResult:      p.TaskResult,
	})
}

func (s *KnowledgeService) RecordUsageFromTaskResult(ctx context.Context, task db.AgentTaskQueue, result []byte) (int, error) {
	workspaceID, err := s.taskWorkspaceID(ctx, task)
	if err != nil {
		return 0, err
	}
	referenceText := extractKnowledgeReferenceText(result)
	ids := extractReferencedKnowledgeIDs(referenceText)
	if len(ids) == 0 {
		return 0, nil
	}
	items, err := s.Queries.ListKnowledgeItemsByIDs(ctx, db.ListKnowledgeItemsByIDsParams{
		WorkspaceID: workspaceID,
		Ids:         ids,
	})
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		if _, err := s.RecordUsage(ctx, KnowledgeUsageParams{
			WorkspaceID:     workspaceID,
			KnowledgeItemID: item.ID,
			AgentTaskID:     task.ID,
			UsageSource:     "agent_reference",
			ReferenceText:   referenceText,
			TaskStatus:      task.Status,
			TaskResult:      result,
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *KnowledgeService) ListAnalytics(ctx context.Context, p KnowledgeAnalyticsParams) ([]db.ListKnowledgeAnalyticsRow, error) {
	if p.Since.IsZero() {
		p.Since = time.Now().AddDate(0, 0, -30)
	}
	if p.Until.IsZero() {
		p.Until = time.Now().Add(24 * time.Hour)
	}
	if !p.Until.After(p.Since) {
		return nil, validationError("until must be after since")
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	return s.Queries.ListKnowledgeAnalytics(ctx, db.ListKnowledgeAnalyticsParams{
		WorkspaceID:     p.WorkspaceID,
		KnowledgeItemID: p.KnowledgeItemID,
		ProjectID:       p.ProjectID,
		AgentID:         p.AgentID,
		Since:           pgtype.Timestamptz{Time: p.Since, Valid: true},
		Until:           pgtype.Timestamptz{Time: p.Until, Valid: true},
		IncludeZero:     p.IncludeZero,
		Limit:           p.Limit,
		Offset:          p.Offset,
	})
}

func (s *KnowledgeService) ListKnowledgeEffect(ctx context.Context, p KnowledgeEffectParams) ([]db.ListKnowledgeEffectHourlyRow, error) {
	if p.Since.IsZero() {
		p.Since = time.Now().AddDate(0, 0, -30)
	}
	if p.Until.IsZero() {
		p.Until = time.Now().Add(24 * time.Hour)
	}
	if !p.Until.After(p.Since) {
		return nil, validationError("until must be after since")
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Limit > 500 {
		p.Limit = 500
	}
	return s.Queries.ListKnowledgeEffectHourly(ctx, db.ListKnowledgeEffectHourlyParams{
		WorkspaceID:  p.WorkspaceID,
		AgentID:      p.AgentID,
		ProjectID:    p.ProjectID,
		TaskKind:     p.TaskKind,
		HasInjection: p.HasInjection,
		Model:        p.Model,
		Since:        pgtype.Timestamptz{Time: p.Since, Valid: true},
		Until:        pgtype.Timestamptz{Time: p.Until, Valid: true},
		Limit:        p.Limit,
		Offset:       p.Offset,
	})
}

func (s *KnowledgeService) GetKnowledgeEffectSummary(ctx context.Context, p KnowledgeEffectSummaryParams) (db.GetKnowledgeEffectSummaryRow, error) {
	if p.Since.IsZero() {
		p.Since = time.Now().AddDate(0, 0, -30)
	}
	if p.Until.IsZero() {
		p.Until = time.Now().Add(24 * time.Hour)
	}
	if !p.Until.After(p.Since) {
		return db.GetKnowledgeEffectSummaryRow{}, validationError("until must be after since")
	}
	return s.Queries.GetKnowledgeEffectSummary(ctx, db.GetKnowledgeEffectSummaryParams{
		WorkspaceID:  p.WorkspaceID,
		AgentID:      p.AgentID,
		ProjectID:    p.ProjectID,
		TaskKind:     p.TaskKind,
		HasInjection: p.HasInjection,
		Model:        p.Model,
		Since:        pgtype.Timestamptz{Time: p.Since, Valid: true},
		Until:        pgtype.Timestamptz{Time: p.Until, Valid: true},
	})
}

func (s *KnowledgeService) RunGovernance(ctx context.Context, p KnowledgeGovernanceParams) (KnowledgeGovernanceResult, error) {
	if p.Limit <= 0 {
		p.Limit = 250
	}
	if p.Limit > 1000 {
		p.Limit = 1000
	}
	rows, err := s.Queries.ListKnowledgeGovernanceCandidates(ctx, db.ListKnowledgeGovernanceCandidatesParams{
		WorkspaceID: p.WorkspaceID,
		Limit:       p.Limit,
	})
	if err != nil {
		return KnowledgeGovernanceResult{}, err
	}
	conflicts := detectKnowledgeConflicts(rows)
	result := KnowledgeGovernanceResult{Checked: len(rows)}
	for _, row := range rows {
		assessment := assessKnowledgeGovernance(row, conflicts[util.UUIDToString(row.ID)])
		if assessment.reviewReason != "" {
			result.ReviewNeeded++
		}
		if assessment.conflictGroup != "" {
			result.Conflicts++
		}
		if _, err := s.Queries.UpdateKnowledgeGovernance(ctx, db.UpdateKnowledgeGovernanceParams{
			ID:                 row.ID,
			WorkspaceID:        row.WorkspaceID,
			StaleScore:         numericFromFloat(assessment.staleScore),
			EffectivenessScore: numericFromFloat(assessment.effectivenessScore),
			ConflictGroup:      governanceText(assessment.conflictGroup),
			ReviewReason:       governanceText(assessment.reviewReason),
			UpdateSuggestion:   governanceText(assessment.updateSuggestion),
		}); err != nil {
			return result, err
		}
		findings, err := s.upsertGovernanceFindings(ctx, row, assessment)
		if err != nil {
			return result, err
		}
		result.Findings += findings
	}
	return result, nil
}

func (s *KnowledgeService) ListGovernanceFindings(ctx context.Context, p KnowledgeGovernanceFindingListParams) ([]db.KnowledgeGovernanceFinding, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	status := textFromTrimmed(p.Status)
	activeOnly := status.Valid && status.String == "active"
	if activeOnly {
		status = pgtype.Text{}
	}
	if status.Valid && !validKnowledgeGovernanceFindingStatus(status.String) {
		return nil, validationError("invalid status")
	}
	findingType := textFromTrimmed(p.FindingType)
	if findingType.Valid && !validKnowledgeGovernanceFindingType(findingType.String) {
		return nil, validationError("invalid finding_type")
	}
	findings, err := s.Queries.ListKnowledgeGovernanceFindings(ctx, db.ListKnowledgeGovernanceFindingsParams{
		WorkspaceID:     p.WorkspaceID,
		Status:          status,
		FindingType:     findingType,
		KnowledgeItemID: p.KnowledgeItemID,
		Limit:           p.Limit,
		Offset:          p.Offset,
	})
	if err != nil || !activeOnly {
		return findings, err
	}
	out := findings[:0]
	for _, finding := range findings {
		if finding.Status == "open" || finding.Status == "drafted" {
			out = append(out, finding)
		}
	}
	return out, nil
}

func (s *KnowledgeService) GetGovernanceFinding(ctx context.Context, workspaceID, findingID pgtype.UUID) (db.KnowledgeGovernanceFinding, error) {
	finding, err := s.Queries.GetKnowledgeGovernanceFinding(ctx, db.GetKnowledgeGovernanceFindingParams{ID: findingID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.KnowledgeGovernanceFinding{}, ErrKnowledgeNotFound
		}
		return db.KnowledgeGovernanceFinding{}, err
	}
	return finding, nil
}

func (s *KnowledgeService) ResolveGovernanceFinding(ctx context.Context, p KnowledgeGovernanceFindingActionParams) (db.KnowledgeGovernanceFinding, error) {
	if !validKnowledgeGovernanceFindingAction(p.Action) {
		return db.KnowledgeGovernanceFinding{}, validationError("invalid governance action")
	}
	finding, err := s.GetGovernanceFinding(ctx, p.WorkspaceID, p.FindingID)
	if err != nil {
		return db.KnowledgeGovernanceFinding{}, err
	}
	switch p.Action {
	case "dismiss":
		return s.Queries.UpdateKnowledgeGovernanceFindingStatus(ctx, db.UpdateKnowledgeGovernanceFindingStatusParams{
			ID:          p.FindingID,
			WorkspaceID: p.WorkspaceID,
			Status:      "dismissed",
			ActorID:     p.ActorID,
		})
	case "reject":
		return s.Queries.UpdateKnowledgeGovernanceFindingStatus(ctx, db.UpdateKnowledgeGovernanceFindingStatusParams{
			ID:          p.FindingID,
			WorkspaceID: p.WorkspaceID,
			Status:      "rejected",
			ActorID:     p.ActorID,
		})
	case "accept":
		if !finding.DraftKnowledgeItemID.Valid {
			return db.KnowledgeGovernanceFinding{}, validationError("governance finding has no update draft")
		}
		if _, err := s.Review(ctx, p.WorkspaceID, finding.DraftKnowledgeItemID, p.ActorID); err != nil {
			return db.KnowledgeGovernanceFinding{}, err
		}
		return s.Queries.UpdateKnowledgeGovernanceFindingStatus(ctx, db.UpdateKnowledgeGovernanceFindingStatusParams{
			ID:                   p.FindingID,
			WorkspaceID:          p.WorkspaceID,
			Status:               "accepted",
			DraftKnowledgeItemID: finding.DraftKnowledgeItemID,
			ActorID:              p.ActorID,
		})
	case "archive":
		if _, err := s.Archive(ctx, p.WorkspaceID, finding.KnowledgeItemID, p.ActorID); err != nil {
			return db.KnowledgeGovernanceFinding{}, err
		}
		return s.Queries.UpdateKnowledgeGovernanceFindingStatus(ctx, db.UpdateKnowledgeGovernanceFindingStatusParams{
			ID:          p.FindingID,
			WorkspaceID: p.WorkspaceID,
			Status:      "archived",
			ActorID:     p.ActorID,
		})
	case "deprecate":
		if _, err := s.Deprecate(ctx, p.WorkspaceID, finding.KnowledgeItemID, p.ActorID); err != nil {
			return db.KnowledgeGovernanceFinding{}, err
		}
		return s.Queries.UpdateKnowledgeGovernanceFindingStatus(ctx, db.UpdateKnowledgeGovernanceFindingStatusParams{
			ID:          p.FindingID,
			WorkspaceID: p.WorkspaceID,
			Status:      "deprecated",
			ActorID:     p.ActorID,
		})
	default:
		return db.KnowledgeGovernanceFinding{}, validationError("invalid governance action")
	}
}

func buildTaskClaimKnowledgeQuery(p KnowledgeTaskClaimParams) string {
	var parts []string
	add := func(label, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, label+": "+value)
		}
	}
	if p.Issue != nil {
		if p.IssueIdentifier != "" {
			add("Issue", p.IssueIdentifier)
		}
		add("Issue title", p.Issue.Title)
		if p.Issue.Description.Valid {
			add("Issue description", p.Issue.Description.String)
		}
		add("Issue status", p.Issue.Status)
		add("Issue priority", p.Issue.Priority)
	}
	if len(p.IssueLabels) > 0 {
		add("Issue labels", strings.Join(p.IssueLabels, ", "))
	}
	add("Project", p.ProjectTitle)
	if len(p.ProjectResources) > 0 {
		resourceParts := make([]string, 0, len(p.ProjectResources))
		for _, resource := range p.ProjectResources {
			part := strings.TrimSpace(resource.ResourceType)
			if resource.Label != "" {
				part += " " + strings.TrimSpace(resource.Label)
			}
			if strings.TrimSpace(part) != "" {
				resourceParts = append(resourceParts, part)
			}
		}
		add("Project resources", strings.Join(resourceParts, ", "))
	}
	add("Trigger comment", p.TriggerCommentContent)
	if p.NewCommentCount > 0 {
		add("New comments context", fmt.Sprintf("%d issue comments since %s after the previous run", p.NewCommentCount, p.NewCommentsSince))
	}
	add("Chat message", p.ChatMessage)
	add("Autopilot title", p.AutopilotTitle)
	add("Autopilot description", p.AutopilotDescription)
	add("Autopilot source", p.AutopilotSource)
	add("Autopilot trigger payload", p.AutopilotTriggerPayload)
	add("Quick create prompt", p.QuickCreatePrompt)
	if len(p.LastTaskResult) > 0 {
		add("Last task result", truncateKnowledgeText(string(p.LastTaskResult), 800))
	}
	add("Last task error", p.LastTaskError)
	add("Last task failure reason", p.LastTaskFailureReason)
	return truncateKnowledgeText(strings.Join(parts, "\n"), 6000)
}

func buildInteractiveKnowledgeSearchQuery(p KnowledgeSearchParams) string {
	if p.Issue == nil {
		return strings.TrimSpace(p.Query)
	}
	var parts []string
	add := func(label, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, label+": "+value)
		}
	}
	add("Query", p.Query)
	if p.Issue != nil {
		add("Issue title", p.Issue.Title)
		if p.Issue.Description.Valid {
			add("Issue description", p.Issue.Description.String)
		}
		add("Issue status", p.Issue.Status)
		add("Issue priority", p.Issue.Priority)
	}
	return truncateKnowledgeText(strings.Join(parts, "\n"), 6000)
}

func (s *KnowledgeService) sourceIssueIdentifier(ctx context.Context, workspaceID, itemID pgtype.UUID) string {
	sources, err := s.Queries.ListKnowledgeSources(ctx, db.ListKnowledgeSourcesParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return ""
	}
	workspace, wsErr := s.Queries.GetWorkspace(ctx, workspaceID)
	prefix := ""
	if wsErr == nil {
		prefix = workspace.IssuePrefix
	}
	for _, source := range sources {
		if source.SourceType != "issue" || !source.SourceID.Valid {
			continue
		}
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: source.SourceID, WorkspaceID: workspaceID})
		if err != nil {
			continue
		}
		if prefix != "" {
			return fmt.Sprintf("%s-%d", prefix, issue.Number)
		}
		return util.UUIDToString(issue.ID)
	}
	for _, source := range sources {
		if source.SourceTitle.Valid && strings.TrimSpace(source.SourceTitle.String) != "" {
			return strings.TrimSpace(source.SourceTitle.String)
		}
		if source.SourceUrl.Valid && strings.TrimSpace(source.SourceUrl.String) != "" {
			return strings.TrimSpace(source.SourceUrl.String)
		}
	}
	return ""
}

func taskClaimInjectionReason(result KnowledgeSearchResult) string {
	if result.MatchReason == "" {
		return "matched task claim context"
	}
	return "matched task claim context via " + result.MatchReason
}

type knowledgeGovernanceAssessment struct {
	staleScore         float64
	effectivenessScore float64
	conflictGroup      string
	reviewReason       string
	updateSuggestion   string
}

func assessKnowledgeGovernance(row db.ListKnowledgeGovernanceCandidatesRow, conflictGroup string) knowledgeGovernanceAssessment {
	negative := row.NotHelpfulCount + row.MisleadingCount + row.OutdatedCount
	totalFeedback := row.HelpfulCount + negative
	staleScore := math.Min(100, float64(row.OutdatedCount)*35)
	if row.LatestNegativeFeedbackAt.Valid && row.LatestNegativeFeedbackAt.Time.After(row.UpdatedAt.Time) {
		staleScore = math.Max(staleScore, 35)
	}
	if row.UpdatedAt.Valid && time.Since(row.UpdatedAt.Time) > 180*24*time.Hour {
		staleScore = math.Min(100, staleScore+20)
	}

	effectiveness := 100.0
	if totalFeedback > 0 {
		effectiveness -= (float64(negative) / float64(totalFeedback)) * 55
	}
	if row.MisleadingCount > 0 {
		effectiveness -= math.Min(30, float64(row.MisleadingCount)*15)
	}
	if row.InjectionCount >= 5 && row.UsageCount == 0 {
		effectiveness = math.Min(effectiveness, 60)
	}
	if row.InjectionCount >= 10 && row.HelpfulCount == 0 && negative > 0 {
		effectiveness = math.Min(effectiveness, 45)
	}
	effectiveness = clampFloat(effectiveness, 0, 100)

	var reasons []string
	var suggestions []string
	if conflictGroup != "" {
		reasons = append(reasons, "conflict_detected")
		suggestions = append(suggestions, "Review conflicting knowledge before publishing an update; do not automatically overwrite the current item.")
	}
	if staleScore >= 70 {
		reasons = append(reasons, "stale_feedback")
		suggestions = append(suggestions, "Ask the Curator to draft an updated version from the latest successful source before this item is injected again.")
	}
	if effectiveness <= 50 && negative >= 2 {
		reasons = append(reasons, "low_effectiveness")
		suggestions = append(suggestions, "Lower default injection priority and inspect negative feedback before re-publishing.")
	}
	if row.MisleadingCount > 0 {
		reasons = append(reasons, "misleading_feedback")
	}
	return knowledgeGovernanceAssessment{
		staleScore:         staleScore,
		effectivenessScore: effectiveness,
		conflictGroup:      conflictGroup,
		reviewReason:       strings.Join(uniqueSignals(reasons), ","),
		updateSuggestion:   strings.Join(uniqueSignals(suggestions), " "),
	}
}

func (s *KnowledgeService) upsertGovernanceFindings(ctx context.Context, row db.ListKnowledgeGovernanceCandidatesRow, assessment knowledgeGovernanceAssessment) (int, error) {
	inputs := governanceFindingInputs(row, assessment)
	if len(inputs) == 0 {
		return 0, nil
	}
	sourceMap, err := s.knowledgeGovernanceSourceMap(ctx, row)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, input := range inputs {
		evidence := cloneStringAnyMap(input.Evidence)
		evidence["source_map"] = sourceMap
		rawEvidence, err := json.Marshal(evidence)
		if err != nil {
			return count, err
		}
		rawSourceMap, err := json.Marshal(sourceMap)
		if err != nil {
			return count, err
		}
		if _, err := s.Queries.UpsertKnowledgeGovernanceFinding(ctx, db.UpsertKnowledgeGovernanceFindingParams{
			WorkspaceID:     row.WorkspaceID,
			KnowledgeItemID: row.ID,
			FindingType:     input.FindingType,
			Severity:        input.Severity,
			Reason:          input.Reason,
			Evidence:        rawEvidence,
			SuggestedAction: input.SuggestedAction,
			SourceMap:       rawSourceMap,
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func governanceFindingInputs(row db.ListKnowledgeGovernanceCandidatesRow, assessment knowledgeGovernanceAssessment) []KnowledgeGovernanceFindingInput {
	negative := row.NotHelpfulCount + row.MisleadingCount + row.OutdatedCount
	baseEvidence := map[string]any{
		"stale_score":                 assessment.staleScore,
		"effectiveness_score":         assessment.effectivenessScore,
		"helpful_count":               row.HelpfulCount,
		"not_helpful_count":           row.NotHelpfulCount,
		"misleading_count":            row.MisleadingCount,
		"outdated_count":              row.OutdatedCount,
		"retrieval_count":             row.RetrievalCount,
		"injection_count":             row.InjectionCount,
		"usage_count":                 row.UsageCount,
		"latest_negative_feedback_at": timestamptzEvidence(row.LatestNegativeFeedbackAt),
		"conflict_group":              assessment.conflictGroup,
		"review_reason":               assessment.reviewReason,
	}
	var out []KnowledgeGovernanceFindingInput
	add := func(kind string, severity int32, reason, action string, extra map[string]any) {
		evidence := cloneStringAnyMap(baseEvidence)
		for k, v := range extra {
			evidence[k] = v
		}
		out = append(out, KnowledgeGovernanceFindingInput{
			FindingType:     kind,
			Severity:        severity,
			Reason:          reason,
			SuggestedAction: action,
			Evidence:        evidence,
		})
	}
	if assessment.conflictGroup != "" {
		add("conflict", 95, "conflict_detected", "Compare the conflicting knowledge items and create a reviewed replacement draft before further RAG use.", nil)
	}
	if assessment.staleScore >= 70 {
		add("stale", int32(math.Round(assessment.staleScore)), "stale_feedback", "Generate an update draft from the latest source evidence; keep the draft in human review.", nil)
	}
	if assessment.effectivenessScore <= 50 && negative >= 2 {
		add("low_effectiveness", int32(math.Round(100-assessment.effectivenessScore)), "low_effectiveness", "Inspect negative feedback and lower confidence or accept an updated draft.", map[string]any{"negative_feedback_count": negative})
	}
	if row.MisleadingCount > 0 {
		add("misleading", int32(clampFloat(float64(row.MisleadingCount)*35, 35, 100)), "misleading_feedback", "Generate a corrective draft from the bad case and prevent automatic publishing.", map[string]any{"misleading_count": row.MisleadingCount})
	}
	if row.OutdatedCount > 0 {
		add("outdated", int32(clampFloat(float64(row.OutdatedCount)*35, 35, 100)), "outdated_feedback", "Refresh the knowledge from current issue/task evidence and route it through review.", map[string]any{"outdated_count": row.OutdatedCount})
	}
	return out
}

func (s *KnowledgeService) knowledgeGovernanceSourceMap(ctx context.Context, row db.ListKnowledgeGovernanceCandidatesRow) (map[string]any, error) {
	sources, err := s.Queries.ListKnowledgeSources(ctx, db.ListKnowledgeSourcesParams{KnowledgeItemID: row.ID, WorkspaceID: row.WorkspaceID})
	if err != nil {
		return nil, err
	}
	feedback, err := s.Queries.ListNegativeKnowledgeFeedback(ctx, db.ListNegativeKnowledgeFeedbackParams{
		WorkspaceID:     row.WorkspaceID,
		KnowledgeItemID: row.ID,
		Limit:           20,
	})
	if err != nil {
		return nil, err
	}
	sourceEntries := make([]map[string]any, 0, len(sources))
	sourceIssueIDs := []string{}
	for _, source := range sources {
		entry := map[string]any{
			"source_type": source.SourceType,
			"source_id":   uuidEvidence(source.SourceID),
			"source_url":  textEvidence(source.SourceUrl),
			"title":       textEvidence(source.SourceTitle),
			"excerpt":     textEvidence(source.SourceExcerpt),
		}
		sourceEntries = append(sourceEntries, entry)
		if source.SourceType == "issue" && source.SourceID.Valid {
			sourceIssueIDs = append(sourceIssueIDs, util.UUIDToString(source.SourceID))
		}
	}
	feedbackEntries := make([]map[string]any, 0, len(feedback))
	for _, item := range feedback {
		feedbackEntries = append(feedbackEntries, map[string]any{
			"id":            util.UUIDToString(item.ID),
			"value":         item.Value,
			"note":          textEvidence(item.Note),
			"agent_task_id": uuidEvidence(item.AgentTaskID),
			"created_at":    timestamptzEvidence(item.CreatedAt),
		})
	}
	return map[string]any{
		"original_knowledge": map[string]any{
			"id":               util.UUIDToString(row.ID),
			"title":            row.Title,
			"lifecycle_status": row.LifecycleStatus,
		},
		"sources":           sourceEntries,
		"source_issue_ids":  sourceIssueIDs,
		"negative_feedback": feedbackEntries,
	}, nil
}

func detectKnowledgeConflicts(rows []db.ListKnowledgeGovernanceCandidatesRow) map[string]string {
	type candidate struct {
		id             string
		recommendation string
	}
	groups := map[string][]candidate{}
	for _, row := range rows {
		if row.LifecycleStatus != "published" && row.LifecycleStatus != "reviewed" {
			continue
		}
		key := knowledgeConflictSignature(row)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], candidate{
			id:             util.UUIDToString(row.ID),
			recommendation: normalizeGovernanceText(row.RecommendedPractice),
		})
	}
	out := map[string]string{}
	for key, candidates := range groups {
		if len(candidates) < 2 {
			continue
		}
		distinct := map[string]bool{}
		for _, candidate := range candidates {
			if candidate.recommendation != "" {
				distinct[candidate.recommendation] = true
			}
		}
		if len(distinct) < 2 {
			continue
		}
		groupID := "conflict:" + truncateKnowledgeText(key, 80)
		for _, candidate := range candidates {
			out[candidate.id] = groupID
		}
	}
	return out
}

func knowledgeConflictSignature(row db.ListKnowledgeGovernanceCandidatesRow) string {
	base := firstNonEmpty(row.ProblemPattern, row.TriggerConditions, row.Title)
	base = normalizeGovernanceText(base)
	if len(base) < 20 {
		return ""
	}
	labels := append([]string{}, row.DomainLabels...)
	sort.Strings(labels)
	if len(labels) > 3 {
		labels = labels[:3]
	}
	return row.Type + ":" + strings.Join(labels, ",") + ":" + truncateKnowledgeText(base, 120)
}

func normalizeGovernanceText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = knowledgeSlugNonAlnum.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func applyKnowledgeGovernanceScore(item db.KnowledgeItem, score float64) float64 {
	effectiveness := numericToFloat(item.EffectivenessScore, 100)
	multiplier := clampFloat(effectiveness/100, 0.2, 1)
	if item.ReviewNeededAt.Valid {
		switch {
		case item.ConflictGroup.Valid && strings.TrimSpace(item.ConflictGroup.String) != "":
			multiplier *= 0.25
		case numericToFloat(item.StaleScore, 0) >= 80:
			multiplier *= 0.35
		default:
			multiplier *= 0.6
		}
	}
	return score * multiplier
}

func eligibleForTaskClaimKnowledge(item db.KnowledgeItem, threshold string) bool {
	if item.LifecycleStatus != "published" || !knowledgeConfidenceAtLeast(item.ConfidenceStatus, threshold) {
		return false
	}
	if item.ConflictGroup.Valid && strings.TrimSpace(item.ConflictGroup.String) != "" {
		return false
	}
	if item.ReviewNeededAt.Valid && numericToFloat(item.StaleScore, 0) >= 80 {
		return false
	}
	return true
}

func knowledgeConfidenceAtLeast(actual, threshold string) bool {
	if threshold == "" {
		threshold = "high"
	}
	rank := map[string]int{"low": 1, "medium": 2, "high": 3}
	minRank, ok := rank[threshold]
	if !ok {
		minRank = rank["high"]
	}
	return rank[actual] >= minRank
}

func numericFromFloat(value float64) pgtype.Numeric {
	value = clampFloat(value, 0, 100)
	return pgtype.Numeric{Int: big.NewInt(int64(math.Round(value * 100))), Exp: -2, Valid: true}
}

func numericToFloat(value pgtype.Numeric, fallback float64) float64 {
	if !value.Valid {
		return fallback
	}
	f, err := value.Float64Value()
	if err != nil || !f.Valid {
		return fallback
	}
	return f.Float64
}

func governanceText(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	return pgtype.Text{String: value, Valid: value != ""}
}

func textFromTrimmed(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	return pgtype.Text{String: value, Valid: value != ""}
}

func textEvidence(value pgtype.Text) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func uuidEvidence(value pgtype.UUID) any {
	if !value.Valid {
		return nil
	}
	return util.UUIDToString(value)
}

func timestamptzEvidence(value pgtype.Timestamptz) any {
	if !value.Valid {
		return nil
	}
	return value.Time.UTC().Format(time.RFC3339Nano)
}

func cloneStringAnyMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func clampFloat(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func validateKnowledgeEnums(itemType, confidence, lifecycle string) error {
	if !validKnowledgeType(itemType) {
		return validationError("invalid type")
	}
	if !validKnowledgeConfidenceStatus(confidence) {
		return validationError("invalid confidence_status")
	}
	if !validKnowledgeLifecycleStatus(lifecycle) {
		return validationError("invalid lifecycle_status")
	}
	return nil
}

func validateFilterEnums(filters KnowledgeSearchFilters) error {
	for _, itemType := range filters.Types {
		if !validKnowledgeType(itemType) {
			return validationError("invalid type")
		}
	}
	for _, status := range filters.Statuses {
		if !validKnowledgeLifecycleStatus(status) {
			return validationError("invalid lifecycle_status")
		}
	}
	return nil
}

func validateOptionalKnowledgeFilters(ctx context.Context, q *db.Queries, workspaceID, projectID, agentID pgtype.UUID) error {
	if projectID.Valid {
		if _, err := q.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return validationError("project not found")
			}
			return err
		}
	}
	if agentID.Valid {
		if _, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return validationError("agent not found")
			}
			return err
		}
	}
	return nil
}

func validateSearchFilters(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, filters KnowledgeSearchFilters) error {
	return validateOptionalKnowledgeFilters(ctx, q, workspaceID, filters.ProjectID, filters.AgentID)
}

func validateKnowledgeSource(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, source KnowledgeSourceInput) error {
	if !validKnowledgeSourceType(source.SourceType) {
		return validationError("invalid source_type")
	}
	if !source.SourceID.Valid && strings.TrimSpace(source.SourceURL.String) == "" {
		return validationError("source_id or source_url is required")
	}
	if !source.SourceID.Valid {
		return nil
	}
	switch source.SourceType {
	case "knowledge":
		_, err := q.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: source.SourceID, WorkspaceID: workspaceID})
		return sourceLookupErr(err)
	case "issue":
		_, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: source.SourceID, WorkspaceID: workspaceID})
		return sourceLookupErr(err)
	case "comment":
		_, err := q.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: source.SourceID, WorkspaceID: workspaceID})
		return sourceLookupErr(err)
	case "agent_task":
		_, err := q.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: source.SourceID, WorkspaceID: workspaceID})
		return sourceLookupErr(err)
	case "pull_request", "commit":
		return nil
	default:
		return validationError("invalid source_type")
	}
}

func sourceLookupErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return validationError("source not found")
	}
	return err
}

func normalizeLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	seen := map[string]bool{}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	return out
}

func validationError(msg string) error {
	return fmt.Errorf("%w: %s", ErrKnowledgeValidation, msg)
}

func validKnowledgeType(v string) bool {
	switch v {
	case "lesson", "playbook", "reference":
		return true
	default:
		return false
	}
}

func validKnowledgeConfidenceStatus(v string) bool {
	switch v {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}

func validKnowledgeLifecycleStatus(v string) bool {
	switch v {
	case "draft", "reviewed", "published", "archived", "deprecated":
		return true
	default:
		return false
	}
}

func validKnowledgeSourceType(v string) bool {
	switch v {
	case "knowledge", "issue", "comment", "agent_task", "pull_request", "commit":
		return true
	default:
		return false
	}
}

func validKnowledgeFeedbackValue(v string) bool {
	switch v {
	case "helpful", "not_helpful", "misleading", "outdated":
		return true
	default:
		return false
	}
}

func validKnowledgeCandidateSourceType(v string) bool {
	switch v {
	case "issue", "comment", "agent_task":
		return true
	default:
		return false
	}
}

func validKnowledgeCandidateStatus(v string) bool {
	switch v {
	case "pending", "accepted", "rejected", "drafted":
		return true
	default:
		return false
	}
}

func validKnowledgeEmbeddingDimension(v int) bool {
	for _, supported := range SupportedKnowledgeEmbeddingDimensions {
		if v == supported {
			return true
		}
	}
	return false
}

func validKnowledgeGovernanceFindingType(v string) bool {
	switch v {
	case "stale", "conflict", "low_effectiveness", "misleading", "outdated":
		return true
	default:
		return false
	}
}

func validKnowledgeGovernanceFindingStatus(v string) bool {
	switch v {
	case "open", "drafted", "accepted", "rejected", "dismissed", "archived", "deprecated":
		return true
	default:
		return false
	}
}

func validKnowledgeGovernanceFindingAction(v string) bool {
	switch v {
	case "accept", "reject", "dismiss", "archive", "deprecate":
		return true
	default:
		return false
	}
}

func knowledgeCandidateDedupeKey(sourceType string, sourceID pgtype.UUID, reason string) string {
	return sourceType + ":" + util.UUIDToString(sourceID) + ":" + reason
}

func extractTaskOutput(result []byte, fallback []byte) string {
	if len(result) == 0 {
		result = fallback
	}
	var payload struct {
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(result, &payload); err == nil {
		return strings.TrimSpace(payload.Output + "\n" + payload.Error)
	}
	return string(result)
}

func looksReusableKnowledge(text string) bool {
	lower := strings.ToLower(text)
	keywords := []string{
		"root cause", "根因", "原因", "fix", "fixed", "修复", "解决", "debug", "诊断",
		"error", "failed", "failure", "报错", "失败", "migration", "config", "permission",
		"token", "workspace", "runtime", "adb", "sql", "query", "test", "command",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func looksLikeUserCorrection(text string) bool {
	lower := strings.ToLower(text)
	keywords := []string{
		"不对", "不是", "还是失败", "仍然失败", "漏了", "正确应该", "应该是", "没生效",
		"wrong", "incorrect", "still fails", "still failing", "missing", "should be",
		"doesn't work", "not working", "regression",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func uniqueSignals(signals []string) []string {
	if len(signals) <= 1 {
		return signals
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(signals))
	for _, signal := range signals {
		if signal == "" || seen[signal] {
			continue
		}
		seen[signal] = true
		out = append(out, signal)
	}
	return out
}

func knowledgeItemFromTextRow(row db.SearchKnowledgeTextRow) db.KnowledgeItem {
	return db.KnowledgeItem{
		ID:                  row.ID,
		WorkspaceID:         row.WorkspaceID,
		ProjectID:           row.ProjectID,
		AgentID:             row.AgentID,
		Title:               row.Title,
		Type:                row.Type,
		DomainLabels:        row.DomainLabels,
		ProblemPattern:      row.ProblemPattern,
		TriggerConditions:   row.TriggerConditions,
		DiagnosticSteps:     row.DiagnosticSteps,
		RecommendedPractice: row.RecommendedPractice,
		AntiPatterns:        row.AntiPatterns,
		Applicability:       row.Applicability,
		ConfidenceStatus:    row.ConfidenceStatus,
		LifecycleStatus:     row.LifecycleStatus,
		CreatedBy:           row.CreatedBy,
		ReviewedBy:          row.ReviewedBy,
		ReviewedAt:          row.ReviewedAt,
		PublishedAt:         row.PublishedAt,
		ArchivedAt:          row.ArchivedAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		UpdatedBy:           row.UpdatedBy,
		DeprecatedAt:        row.DeprecatedAt,
		StaleScore:          row.StaleScore,
		EffectivenessScore:  row.EffectivenessScore,
		ConflictGroup:       row.ConflictGroup,
		ReviewReason:        row.ReviewReason,
		UpdateSuggestion:    row.UpdateSuggestion,
		ReviewNeededAt:      row.ReviewNeededAt,
		GovernanceCheckedAt: row.GovernanceCheckedAt,
	}
}

func knowledgeItemFromVectorRow(row db.SearchKnowledgeVectorRow) db.KnowledgeItem {
	return db.KnowledgeItem{
		ID:                  row.ID,
		WorkspaceID:         row.WorkspaceID,
		ProjectID:           row.ProjectID,
		AgentID:             row.AgentID,
		Title:               row.Title,
		Type:                row.Type,
		DomainLabels:        row.DomainLabels,
		ProblemPattern:      row.ProblemPattern,
		TriggerConditions:   row.TriggerConditions,
		DiagnosticSteps:     row.DiagnosticSteps,
		RecommendedPractice: row.RecommendedPractice,
		AntiPatterns:        row.AntiPatterns,
		Applicability:       row.Applicability,
		ConfidenceStatus:    row.ConfidenceStatus,
		LifecycleStatus:     row.LifecycleStatus,
		CreatedBy:           row.CreatedBy,
		ReviewedBy:          row.ReviewedBy,
		ReviewedAt:          row.ReviewedAt,
		PublishedAt:         row.PublishedAt,
		ArchivedAt:          row.ArchivedAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		UpdatedBy:           row.UpdatedBy,
		DeprecatedAt:        row.DeprecatedAt,
		StaleScore:          row.StaleScore,
		EffectivenessScore:  row.EffectivenessScore,
		ConflictGroup:       row.ConflictGroup,
		ReviewReason:        row.ReviewReason,
		UpdateSuggestion:    row.UpdateSuggestion,
		ReviewNeededAt:      row.ReviewNeededAt,
		GovernanceCheckedAt: row.GovernanceCheckedAt,
	}
}

func knowledgeVectorRowsFrom1536(rows []db.SearchKnowledgeVectorRow) []knowledgeVectorSearchRow {
	out := make([]knowledgeVectorSearchRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, knowledgeVectorSearchRow{
			Item:        knowledgeItemFromVectorRow(row),
			VectorScore: row.VectorScore,
		})
	}
	return out
}

func knowledgeVectorRowsFrom3072(rows []db.SearchKnowledgeVector3072Row) []knowledgeVectorSearchRow {
	out := make([]knowledgeVectorSearchRow, 0, len(rows))
	for _, row := range rows {
		base := db.SearchKnowledgeVectorRow(row)
		out = append(out, knowledgeVectorSearchRow{
			Item:        knowledgeItemFromVectorRow(base),
			VectorScore: base.VectorScore,
		})
	}
	return out
}

func knowledgeVectorRowsFrom1024(rows []db.SearchKnowledgeVector1024Row) []knowledgeVectorSearchRow {
	out := make([]knowledgeVectorSearchRow, 0, len(rows))
	for _, row := range rows {
		base := db.SearchKnowledgeVectorRow(row)
		out = append(out, knowledgeVectorSearchRow{
			Item:        knowledgeItemFromVectorRow(base),
			VectorScore: base.VectorScore,
		})
	}
	return out
}

func knowledgeVectorRowsFrom768(rows []db.SearchKnowledgeVector768Row) []knowledgeVectorSearchRow {
	out := make([]knowledgeVectorSearchRow, 0, len(rows))
	for _, row := range rows {
		base := db.SearchKnowledgeVectorRow(row)
		out = append(out, knowledgeVectorSearchRow{
			Item:        knowledgeItemFromVectorRow(base),
			VectorScore: base.VectorScore,
		})
	}
	return out
}

func upsertKnowledgeEmbeddingRowFrom3072(row db.UpsertKnowledgeEmbedding3072Row) db.UpsertKnowledgeEmbeddingRow {
	return db.UpsertKnowledgeEmbeddingRow(row)
}

func upsertKnowledgeEmbeddingRowFrom1024(row db.UpsertKnowledgeEmbedding1024Row) db.UpsertKnowledgeEmbeddingRow {
	return db.UpsertKnowledgeEmbeddingRow(row)
}

func upsertKnowledgeEmbeddingRowFrom768(row db.UpsertKnowledgeEmbedding768Row) db.UpsertKnowledgeEmbeddingRow {
	return db.UpsertKnowledgeEmbeddingRow(row)
}

func (s *KnowledgeService) setLifecycleStatus(ctx context.Context, q *db.Queries, workspaceID, itemID, actorID pgtype.UUID, next string) (db.KnowledgeItem, error) {
	if !validKnowledgeLifecycleStatus(next) {
		return db.KnowledgeItem{}, validationError("invalid lifecycle_status")
	}
	item, err := q.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return db.KnowledgeItem{}, knowledgeItemLookupErr(err)
	}
	if item.LifecycleStatus == next {
		return item, nil
	}
	if err := validateKnowledgeLifecycleTransition(ctx, q, item, next); err != nil {
		return db.KnowledgeItem{}, err
	}
	var reviewedBy pgtype.UUID
	if next == "reviewed" {
		reviewedBy = actorID
	}
	updated, err := q.SetKnowledgeLifecycleStatus(ctx, db.SetKnowledgeLifecycleStatusParams{
		ID:              itemID,
		WorkspaceID:     workspaceID,
		LifecycleStatus: next,
		ReviewedBy:      reviewedBy,
		UpdatedBy:       actorID,
	})
	if err != nil {
		return db.KnowledgeItem{}, knowledgeItemLookupErr(err)
	}
	return updated, nil
}

func validateKnowledgeLifecycleTransition(ctx context.Context, q *db.Queries, item db.KnowledgeItem, next string) error {
	current := item.LifecycleStatus
	switch next {
	case "draft":
		if current != "archived" && current != "deprecated" {
			return validationError("only archived or deprecated knowledge can be restored")
		}
	case "reviewed":
		if current != "draft" && current != "archived" && current != "deprecated" {
			return validationError("knowledge can only be reviewed from draft or restored from inactive")
		}
	case "published":
		if current != "reviewed" {
			return validationError("knowledge must be reviewed before publishing")
		}
		if !item.ReviewedBy.Valid || !item.ReviewedAt.Valid {
			return validationError("published knowledge requires human review")
		}
		count, err := q.CountKnowledgeSources(ctx, db.CountKnowledgeSourcesParams{KnowledgeItemID: item.ID, WorkspaceID: item.WorkspaceID})
		if err != nil {
			return err
		}
		if count == 0 {
			return validationError("published knowledge requires at least one source")
		}
	case "archived":
		if current == "deprecated" {
			return validationError("deprecated knowledge must be restored before archiving")
		}
	case "deprecated":
		if current != "reviewed" && current != "published" {
			return validationError("only reviewed or published knowledge can be deprecated")
		}
	default:
		return validationError("invalid lifecycle_status")
	}
	return nil
}

func knowledgeItemLookupErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrKnowledgeNotFound
	}
	return err
}

func knowledgePublishTargetErr(err error, notFound string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return validationError(notFound)
	}
	return err
}

func summarizeKnowledgeSources(sources []db.KnowledgeSource) KnowledgeSourceSummary {
	summary := KnowledgeSourceSummary{Count: len(sources)}
	seen := map[string]bool{}
	for i, source := range sources {
		if !seen[source.SourceType] {
			seen[source.SourceType] = true
			summary.Types = append(summary.Types, source.SourceType)
		}
		if i == 0 {
			summary.PrimarySourceType = source.SourceType
			summary.PrimarySourceID = source.SourceID
			if source.SourceTitle.Valid {
				summary.PrimarySourceTitle = source.SourceTitle.String
			}
		}
	}
	sort.Strings(summary.Types)
	return summary
}

func knowledgeWikiContent(item db.KnowledgeItem) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(item.Title)
	b.WriteString("\n\n")
	writeKnowledgeSection(&b, "Problem Pattern", item.ProblemPattern)
	writeKnowledgeSection(&b, "Trigger Conditions", item.TriggerConditions)
	writeKnowledgeSection(&b, "Diagnostic Steps", item.DiagnosticSteps)
	writeKnowledgeSection(&b, "Recommended Practice", item.RecommendedPractice)
	writeKnowledgeSection(&b, "Anti-patterns", item.AntiPatterns)
	writeKnowledgeSection(&b, "Applicability", item.Applicability)
	return strings.TrimSpace(b.String()) + "\n"
}

func knowledgeSkillContent(item db.KnowledgeItem, name, description string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: ")
	b.WriteString(name)
	b.WriteString("\n")
	b.WriteString("description: ")
	b.WriteString(strings.ReplaceAll(description, "\n", " "))
	b.WriteString("\n---\n\n")
	b.WriteString("# ")
	b.WriteString(item.Title)
	b.WriteString("\n\n")
	writeKnowledgeSection(&b, "When To Use", item.TriggerConditions)
	writeKnowledgeSection(&b, "Problem Pattern", item.ProblemPattern)
	writeKnowledgeSection(&b, "Diagnostic Steps", item.DiagnosticSteps)
	writeKnowledgeSection(&b, "Recommended Practice", item.RecommendedPractice)
	writeKnowledgeSection(&b, "Anti-patterns", item.AntiPatterns)
	writeKnowledgeSection(&b, "Applicability", item.Applicability)
	return strings.TrimSpace(b.String()) + "\n"
}

func knowledgeSourceMapContent(item db.KnowledgeItem, itemID pgtype.UUID) string {
	var b strings.Builder
	b.WriteString("# Source Map\n\n")
	b.WriteString("- knowledge_item_id: ")
	b.WriteString(util.UUIDToString(itemID))
	b.WriteString("\n")
	b.WriteString("- title: ")
	b.WriteString(item.Title)
	b.WriteString("\n")
	b.WriteString("- lifecycle_status: ")
	b.WriteString(item.LifecycleStatus)
	b.WriteString("\n")
	return b.String()
}

func writeKnowledgeSection(b *strings.Builder, title, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(value)
	b.WriteString("\n\n")
}

func truncateKnowledgeText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}

func extractKnowledgeReferenceText(result []byte) string {
	var payload struct {
		Output string `json:"output"`
	}
	if len(result) > 0 && json.Unmarshal(result, &payload) == nil && strings.TrimSpace(payload.Output) != "" {
		return payload.Output
	}
	return string(result)
}

func extractReferencedKnowledgeIDs(text string) []pgtype.UUID {
	seen := map[string]struct{}{}
	var ids []pgtype.UUID
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		shouldScan := false
		for _, keyword := range knowledgeUsedLineKeywords {
			if strings.Contains(lower, keyword) {
				shouldScan = true
				break
			}
		}
		if !shouldScan {
			continue
		}
		for _, match := range knowledgeReferenceUUIDRe.FindAllStringSubmatch(line, -1) {
			if len(match) < 2 {
				continue
			}
			id, err := util.ParseUUID(match[1])
			if err != nil {
				continue
			}
			key := util.UUIDToString(id)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

// ListKnowledgeInjectionsByIssue returns non-discarded injection events for
// every agent task on the given issue, enriched with knowledge item metadata
// and usage state.
func (s *KnowledgeService) ListKnowledgeInjectionsByIssue(ctx context.Context, workspaceID, issueID pgtype.UUID) ([]db.ListKnowledgeInjectionsByIssueRow, error) {
	return s.Queries.ListKnowledgeInjectionsByIssue(ctx, db.ListKnowledgeInjectionsByIssueParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
	})
}

func knowledgeSlugFromTitle(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = knowledgeSlugNonAlnum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "knowledge"
	}
	return slug
}

func knowledgeSlugWithSuffix(base string, attempt int) string {
	if attempt <= 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, attempt)
}

func isPgUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
