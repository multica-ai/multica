package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrCuratorEngineUnavailable = errors.New("knowledge curator engine is not configured")

type CuratorEngineInfo struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	EmbeddingModel string `json:"embedding_model,omitempty"`
	RuntimeMode    string `json:"runtime_mode,omitempty"`
}

type CuratorEngine interface {
	GenerateDraft(ctx context.Context, input CuratorDraftInput) (CuratorDraft, error)
	SummarizeSource(ctx context.Context, source CuratorSourceBundle) (string, error)
	BuildEmbedding(ctx context.Context, content string) ([]float32, error)
	Info() CuratorEngineInfo
}

type KnowledgeCuratorService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Knowledge *KnowledgeService
	Engine    CuratorEngine
}

func NewKnowledgeCuratorService(q *db.Queries, tx TxStarter, knowledge *KnowledgeService, engine CuratorEngine) *KnowledgeCuratorService {
	if engine == nil {
		engine = MissingCuratorEngine{}
	}
	return &KnowledgeCuratorService{Queries: q, TxStarter: tx, Knowledge: knowledge, Engine: engine}
}

type CuratorIssueDraftParams struct {
	WorkspaceID pgtype.UUID
	IssueID     pgtype.UUID
	CreatedBy   pgtype.UUID
}

type CuratorCandidateDraftParams struct {
	WorkspaceID pgtype.UUID
	CandidateID pgtype.UUID
	CreatedBy   pgtype.UUID
	Regenerate  bool
}

type CuratorGovernanceDraftParams struct {
	WorkspaceID pgtype.UUID
	FindingID   pgtype.UUID
	CreatedBy   pgtype.UUID
	Regenerate  bool
}

type KnowledgeEmbeddingRebuildParams struct {
	WorkspaceID pgtype.UUID
	ItemID      pgtype.UUID
	Limit       int32
}

type KnowledgeEmbeddingRebuildResult struct {
	Checked int
	Rebuilt int
	Skipped int
	Failed  int
}

type CuratorDraftInput struct {
	WorkspaceID      pgtype.UUID
	Issue            db.Issue
	Project          *db.Project
	Labels           []db.IssueLabel
	Comments         []db.Comment
	AgentTasks       []db.AgentTaskQueue
	PullRequests     []db.ListPullRequestsByIssueRow
	Candidate        *db.KnowledgeCandidate
	Governance       *db.KnowledgeGovernanceFinding
	OriginalItem     *db.KnowledgeItem
	OriginalSources  []db.KnowledgeSource
	NegativeFeedback []db.KnowledgeFeedback
	SourceSummary    string
	TriggerComment   *db.Comment
	TriggerTask      *db.AgentTaskQueue
}

type CuratorSourceBundle struct {
	WorkspaceID  pgtype.UUID
	Issue        db.Issue
	Project      *db.Project
	Labels       []db.IssueLabel
	Comments     []db.Comment
	AgentTasks   []db.AgentTaskQueue
	PullRequests []db.ListPullRequestsByIssueRow
}

type CuratorDraft struct {
	Title               string   `json:"title"`
	Type                string   `json:"type"`
	DomainLabels        []string `json:"domain_labels"`
	ProblemPattern      string   `json:"problem_pattern"`
	TriggerConditions   string   `json:"trigger_conditions"`
	DiagnosticSteps     string   `json:"diagnostic_steps"`
	RecommendedPractice string   `json:"recommended_practice"`
	AntiPatterns        string   `json:"anti_patterns"`
	Applicability       string   `json:"applicability"`
	ConfidenceStatus    string   `json:"confidence_status"`
}

type MissingCuratorEngine struct{}

func (MissingCuratorEngine) GenerateDraft(context.Context, CuratorDraftInput) (CuratorDraft, error) {
	return CuratorDraft{}, ErrCuratorEngineUnavailable
}

func (MissingCuratorEngine) SummarizeSource(context.Context, CuratorSourceBundle) (string, error) {
	return "", ErrCuratorEngineUnavailable
}

func (MissingCuratorEngine) BuildEmbedding(context.Context, string) ([]float32, error) {
	return nil, ErrCuratorEngineUnavailable
}

func (MissingCuratorEngine) Info() CuratorEngineInfo {
	return CuratorEngineInfo{Provider: "missing"}
}

type StaticCuratorEngine struct {
	Draft     CuratorDraft
	Summary   string
	Embedding []float32
	Err       error
	Engine    CuratorEngineInfo
}

func (e StaticCuratorEngine) GenerateDraft(context.Context, CuratorDraftInput) (CuratorDraft, error) {
	if e.Err != nil {
		return CuratorDraft{}, e.Err
	}
	return e.Draft, nil
}

func (e StaticCuratorEngine) SummarizeSource(context.Context, CuratorSourceBundle) (string, error) {
	if e.Err != nil {
		return "", e.Err
	}
	if strings.TrimSpace(e.Summary) != "" {
		return e.Summary, nil
	}
	return "Static curator source summary", nil
}

func (e StaticCuratorEngine) BuildEmbedding(context.Context, string) ([]float32, error) {
	if e.Err != nil {
		return nil, e.Err
	}
	return e.Embedding, nil
}

func (e StaticCuratorEngine) Info() CuratorEngineInfo {
	if strings.TrimSpace(e.Engine.Provider) == "" {
		return CuratorEngineInfo{Provider: "static", Model: "test"}
	}
	return e.Engine
}

func (s *KnowledgeCuratorService) GenerateDraftFromIssue(ctx context.Context, p CuratorIssueDraftParams) (KnowledgeDetail, error) {
	bundle, err := s.collectIssueSource(ctx, p.WorkspaceID, p.IssueID)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	input, err := s.buildDraftInput(ctx, bundle, nil)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	draft, err := s.generateAndValidateDraft(ctx, input)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	return s.createDraft(ctx, p.WorkspaceID, p.CreatedBy, input, draft)
}

func (s *KnowledgeCuratorService) GenerateDraftFromCandidate(ctx context.Context, p CuratorCandidateDraftParams) (KnowledgeDetail, error) {
	candidate, err := s.Queries.GetKnowledgeCandidate(ctx, db.GetKnowledgeCandidateParams{ID: p.CandidateID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeDetail{}, ErrKnowledgeNotFound
		}
		return KnowledgeDetail{}, err
	}
	if candidate.Status == "rejected" {
		return KnowledgeDetail{}, validationError("candidate is rejected")
	}
	if candidate.Status == "drafted" && !p.Regenerate {
		if itemID, ok := candidateKnowledgeItemID(candidate); ok {
			return s.Knowledge.GetDetail(ctx, p.WorkspaceID, itemID)
		}
	}

	bundle, err := s.collectIssueSource(ctx, p.WorkspaceID, candidate.IssueID)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	input, err := s.buildDraftInput(ctx, bundle, &candidate)
	if err != nil {
		_ = s.markCandidateDraftFailed(ctx, candidate, err)
		return KnowledgeDetail{}, err
	}
	draft, err := s.generateAndValidateDraft(ctx, input)
	if err != nil {
		if errors.Is(err, ErrCuratorDraftDispatched) {
			return KnowledgeDetail{}, err
		}
		_ = s.markCandidateDraftFailed(ctx, candidate, err)
		return KnowledgeDetail{}, err
	}
	detail, err := s.createDraft(ctx, p.WorkspaceID, p.CreatedBy, input, draft)
	if err != nil {
		_ = s.markCandidateDraftFailed(ctx, candidate, err)
		return KnowledgeDetail{}, err
	}
	if _, err := s.markCandidateDraftSucceeded(ctx, candidate, detail.Item.ID, input.SourceSummary); err != nil {
		return KnowledgeDetail{}, err
	}
	return detail, nil
}

func (s *KnowledgeCuratorService) RegenerateDraft(ctx context.Context, p CuratorCandidateDraftParams) (KnowledgeDetail, error) {
	p.Regenerate = true
	return s.GenerateDraftFromCandidate(ctx, p)
}

func (s *KnowledgeCuratorService) GenerateDraftFromGovernanceFinding(ctx context.Context, p CuratorGovernanceDraftParams) (KnowledgeDetail, error) {
	finding, err := s.Queries.GetKnowledgeGovernanceFinding(ctx, db.GetKnowledgeGovernanceFindingParams{ID: p.FindingID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeDetail{}, ErrKnowledgeNotFound
		}
		return KnowledgeDetail{}, err
	}
	if finding.Status == "rejected" || finding.Status == "dismissed" || finding.Status == "accepted" || finding.Status == "archived" || finding.Status == "deprecated" {
		return KnowledgeDetail{}, validationError("governance finding is already resolved")
	}
	if finding.Status == "drafted" && !p.Regenerate && finding.DraftKnowledgeItemID.Valid {
		return s.Knowledge.GetDetail(ctx, p.WorkspaceID, finding.DraftKnowledgeItemID)
	}
	item, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: finding.KnowledgeItemID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return KnowledgeDetail{}, knowledgeItemLookupErr(err)
	}
	originalSources, err := s.Queries.ListKnowledgeSources(ctx, db.ListKnowledgeSourcesParams{KnowledgeItemID: item.ID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	issueID := firstIssueSourceID(originalSources)
	if !issueID.Valid {
		return KnowledgeDetail{}, validationError("governance draft requires an issue source")
	}
	bundle, err := s.collectIssueSource(ctx, p.WorkspaceID, issueID)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	input, err := s.buildDraftInput(ctx, bundle, nil)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	feedback, err := s.Queries.ListNegativeKnowledgeFeedback(ctx, db.ListNegativeKnowledgeFeedbackParams{
		WorkspaceID:     p.WorkspaceID,
		KnowledgeItemID: item.ID,
		Limit:           20,
	})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	input.Governance = &finding
	input.OriginalItem = &item
	input.OriginalSources = originalSources
	input.NegativeFeedback = feedback
	input.SourceSummary = strings.TrimSpace(strings.Join([]string{
		input.SourceSummary,
		governanceFindingSummary(finding, item, feedback),
	}, "\n\n"))
	draft, err := s.generateAndValidateDraft(ctx, input)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	detail, err := s.createDraft(ctx, p.WorkspaceID, p.CreatedBy, input, draft)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	updated, err := s.Queries.UpdateKnowledgeGovernanceFindingStatus(ctx, db.UpdateKnowledgeGovernanceFindingStatusParams{
		ID:                   finding.ID,
		WorkspaceID:          finding.WorkspaceID,
		Status:               "drafted",
		DraftKnowledgeItemID: detail.Item.ID,
		ActorID:              p.CreatedBy,
	})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	_ = updated
	return detail, nil
}

func (s *KnowledgeCuratorService) SummarizeSource(ctx context.Context, bundle CuratorSourceBundle) (string, error) {
	engine := s.engineForWorkspace(ctx, bundle.WorkspaceID)
	if engine == nil {
		return deterministicSourceSummary(bundle), nil
	}
	summary, err := engine.SummarizeSource(ctx, bundle)
	if err != nil || strings.TrimSpace(summary) == "" {
		return deterministicSourceSummary(bundle), nil
	}
	return strings.TrimSpace(summary), nil
}

func (s *KnowledgeCuratorService) BuildEmbedding(ctx context.Context, content string) ([]float32, string, error) {
	return s.buildEmbeddingWithEngine(ctx, s.Engine, content)
}

func (s *KnowledgeCuratorService) buildEmbeddingWithEngine(ctx context.Context, engine CuratorEngine, content string) ([]float32, string, error) {
	if engine == nil {
		return nil, "", ErrCuratorEngineUnavailable
	}
	embedding, err := engine.BuildEmbedding(ctx, content)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256([]byte(content))
	return embedding, hex.EncodeToString(sum[:]), nil
}

func (s *KnowledgeCuratorService) EnsureKnowledgeEmbedding(ctx context.Context, workspaceID, itemID pgtype.UUID) (bool, error) {
	item, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return false, knowledgeItemLookupErr(err)
	}
	if item.LifecycleStatus != "reviewed" && item.LifecycleStatus != "published" {
		return false, nil
	}
	engine := s.engineForWorkspace(ctx, workspaceID)
	if engine == nil {
		return false, ErrCuratorEngineUnavailable
	}
	content := canonicalKnowledgeEmbeddingContent(item)
	info := engine.Info()
	if strings.TrimSpace(info.Provider) == "" || strings.TrimSpace(info.EmbeddingModel) == "" {
		return false, ErrCuratorEngineUnavailable
	}
	embeddings, err := s.Queries.ListKnowledgeEmbeddingMetadata(ctx, db.ListKnowledgeEmbeddingMetadataParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256([]byte(content))
	contentHash := hex.EncodeToString(sum[:])
	for _, existing := range embeddings {
		if existing.Provider == info.Provider && existing.Model == info.EmbeddingModel && existing.ContentHash == contentHash {
			return false, nil
		}
	}
	embedding, hash, err := s.buildEmbeddingWithEngine(ctx, engine, content)
	if err != nil {
		return false, err
	}
	if _, err := s.Knowledge.UpsertEmbedding(ctx, itemID, workspaceID, info.Provider, info.EmbeddingModel, hash, embedding); err != nil {
		return false, err
	}
	return true, nil
}

func (s *KnowledgeCuratorService) RebuildKnowledgeEmbeddings(ctx context.Context, p KnowledgeEmbeddingRebuildParams) (KnowledgeEmbeddingRebuildResult, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	items := []db.KnowledgeItem{}
	if p.ItemID.Valid {
		item, err := s.Queries.GetKnowledgeItem(ctx, db.GetKnowledgeItemParams{ID: p.ItemID, WorkspaceID: p.WorkspaceID})
		if err != nil {
			return KnowledgeEmbeddingRebuildResult{}, knowledgeItemLookupErr(err)
		}
		items = append(items, item)
	} else {
		listed, err := s.Queries.ListKnowledgeItemsForEmbeddingRebuild(ctx, db.ListKnowledgeItemsForEmbeddingRebuildParams{
			WorkspaceID: p.WorkspaceID,
			Limit:       limit,
		})
		if err != nil {
			return KnowledgeEmbeddingRebuildResult{}, err
		}
		items = listed
	}
	result := KnowledgeEmbeddingRebuildResult{Checked: len(items)}
	for _, item := range items {
		rebuilt, err := s.EnsureKnowledgeEmbedding(ctx, item.WorkspaceID, item.ID)
		if err != nil {
			result.Failed++
			continue
		}
		if rebuilt {
			result.Rebuilt++
		} else {
			result.Skipped++
		}
	}
	return result, nil
}

func canonicalKnowledgeEmbeddingContent(item db.KnowledgeItem) string {
	return strings.Join([]string{
		"Title: " + item.Title,
		"Type: " + item.Type,
		"Labels: " + strings.Join(item.DomainLabels, ", "),
		"Problem pattern:\n" + item.ProblemPattern,
		"Trigger conditions:\n" + item.TriggerConditions,
		"Diagnostic steps:\n" + item.DiagnosticSteps,
		"Recommended practice:\n" + item.RecommendedPractice,
		"Anti-patterns:\n" + item.AntiPatterns,
		"Applicability:\n" + item.Applicability,
	}, "\n\n")
}

type workspaceCuratorEngine interface {
	ForWorkspace(context.Context, pgtype.UUID) CuratorEngine
}

func (s *KnowledgeCuratorService) engineForWorkspace(ctx context.Context, workspaceID pgtype.UUID) CuratorEngine {
	if s.Engine == nil {
		return nil
	}
	if workspaceID.Valid {
		if engine, ok := s.Engine.(workspaceCuratorEngine); ok {
			return engine.ForWorkspace(ctx, workspaceID)
		}
	}
	return s.Engine
}

func (s *KnowledgeCuratorService) collectIssueSource(ctx context.Context, workspaceID, issueID pgtype.UUID) (CuratorSourceBundle, error) {
	issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
	if err != nil {
		return CuratorSourceBundle{}, sourceLookupErr(err)
	}

	var project *db.Project
	if issue.ProjectID.Valid {
		if p, err := s.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: issue.ProjectID, WorkspaceID: workspaceID}); err == nil {
			project = &p
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return CuratorSourceBundle{}, err
		}
	}

	labels, err := s.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{IssueID: issue.ID, WorkspaceID: workspaceID})
	if err != nil {
		return CuratorSourceBundle{}, err
	}
	comments, err := s.Queries.ListIssueCommentsForKnowledgeDraft(ctx, db.ListIssueCommentsForKnowledgeDraftParams{
		WorkspaceID: workspaceID,
		IssueID:     issue.ID,
		Limit:       200,
	})
	if err != nil {
		return CuratorSourceBundle{}, err
	}
	tasks, err := s.Queries.ListIssueAgentTasksForKnowledgeDraft(ctx, db.ListIssueAgentTasksForKnowledgeDraftParams{
		WorkspaceID: workspaceID,
		IssueID:     issue.ID,
		Limit:       100,
	})
	if err != nil {
		return CuratorSourceBundle{}, err
	}
	pulls, err := s.Queries.ListPullRequestsByIssue(ctx, issue.ID)
	if err != nil {
		return CuratorSourceBundle{}, err
	}
	filteredPulls := pulls[:0]
	for _, pr := range pulls {
		if pr.WorkspaceID == workspaceID {
			filteredPulls = append(filteredPulls, pr)
		}
	}

	return CuratorSourceBundle{
		WorkspaceID:  workspaceID,
		Issue:        issue,
		Project:      project,
		Labels:       labels,
		Comments:     prioritizeKnowledgeComments(comments, pgtype.UUID{}),
		AgentTasks:   tasks,
		PullRequests: filteredPulls,
	}, nil
}

func (s *KnowledgeCuratorService) buildDraftInput(ctx context.Context, bundle CuratorSourceBundle, candidate *db.KnowledgeCandidate) (CuratorDraftInput, error) {
	var triggerComment *db.Comment
	var triggerTask *db.AgentTaskQueue
	triggerCommentID := pgtype.UUID{}
	if candidate != nil {
		triggerCommentID = candidate.CommentID
		for i := range bundle.AgentTasks {
			if candidate.AgentTaskID.Valid && bundle.AgentTasks[i].ID == candidate.AgentTaskID {
				triggerTask = &bundle.AgentTasks[i]
				if bundle.AgentTasks[i].TriggerCommentID.Valid {
					triggerCommentID = bundle.AgentTasks[i].TriggerCommentID
				}
				break
			}
		}
	}
	if triggerCommentID.Valid {
		for i := range bundle.Comments {
			if bundle.Comments[i].ID == triggerCommentID {
				triggerComment = &bundle.Comments[i]
				break
			}
		}
		bundle.Comments = prioritizeKnowledgeComments(bundle.Comments, triggerCommentID)
	}
	summary, err := s.SummarizeSource(ctx, bundle)
	if err != nil {
		return CuratorDraftInput{}, err
	}
	return CuratorDraftInput{
		WorkspaceID:    bundle.WorkspaceID,
		Issue:          bundle.Issue,
		Project:        bundle.Project,
		Labels:         bundle.Labels,
		Comments:       bundle.Comments,
		AgentTasks:     bundle.AgentTasks,
		PullRequests:   bundle.PullRequests,
		Candidate:      candidate,
		SourceSummary:  summary,
		TriggerComment: triggerComment,
		TriggerTask:    triggerTask,
	}, nil
}

func (s *KnowledgeCuratorService) generateAndValidateDraft(ctx context.Context, input CuratorDraftInput) (CuratorDraft, error) {
	engine := s.engineForWorkspace(ctx, input.WorkspaceID)
	if engine == nil {
		return CuratorDraft{}, ErrCuratorEngineUnavailable
	}
	draft, err := engine.GenerateDraft(ctx, input)
	if err != nil {
		return CuratorDraft{}, err
	}
	if err := validateCuratorDraft(draft); err != nil {
		return CuratorDraft{}, err
	}
	return draft, nil
}

func (s *KnowledgeCuratorService) createDraft(ctx context.Context, workspaceID, createdBy pgtype.UUID, input CuratorDraftInput, draft CuratorDraft) (KnowledgeDetail, error) {
	return s.Knowledge.Create(ctx, KnowledgeCreateParams{
		WorkspaceID:         workspaceID,
		ProjectID:           input.Issue.ProjectID,
		Title:               draft.Title,
		Type:                draft.Type,
		DomainLabels:        draft.DomainLabels,
		ProblemPattern:      draft.ProblemPattern,
		TriggerConditions:   draft.TriggerConditions,
		DiagnosticSteps:     draft.DiagnosticSteps,
		RecommendedPractice: draft.RecommendedPractice,
		AntiPatterns:        draft.AntiPatterns,
		Applicability:       draft.Applicability,
		ConfidenceStatus:    draft.ConfidenceStatus,
		LifecycleStatus:     "draft",
		CreatedBy:           createdBy,
		Sources:             curatorKnowledgeSources(input),
	})
}

func (s *KnowledgeCuratorService) markCandidateDraftSucceeded(ctx context.Context, candidate db.KnowledgeCandidate, itemID pgtype.UUID, summary string) (db.KnowledgeCandidate, error) {
	meta := candidateMetadata(candidate)
	engine := s.engineForWorkspace(ctx, candidate.WorkspaceID)
	meta["knowledge_item_id"] = util.UUIDToString(itemID)
	meta["draft_error"] = nil
	meta["source_summary"] = summary
	meta["draft_generation"] = map[string]any{
		"status":       "succeeded",
		"knowledge_id": util.UUIDToString(itemID),
		"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"engine":       curatorEngineInfo(engine),
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return db.KnowledgeCandidate{}, err
	}
	return s.Queries.UpdateKnowledgeCandidateDraftState(ctx, db.UpdateKnowledgeCandidateDraftStateParams{
		Status:      pgtype.Text{String: "drafted", Valid: true},
		Metadata:    raw,
		ID:          candidate.ID,
		WorkspaceID: candidate.WorkspaceID,
	})
}

func (s *KnowledgeCuratorService) markCandidateDraftFailed(ctx context.Context, candidate db.KnowledgeCandidate, cause error) error {
	meta := candidateMetadata(candidate)
	engine := s.engineForWorkspace(ctx, candidate.WorkspaceID)
	message := "draft generation failed"
	if cause != nil {
		message = cause.Error()
	}
	meta["draft_error"] = message
	meta["draft_generation"] = map[string]any{
		"status":       "failed",
		"error":        message,
		"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"engine":       curatorEngineInfo(engine),
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = s.Queries.UpdateKnowledgeCandidateDraftState(ctx, db.UpdateKnowledgeCandidateDraftStateParams{
		Metadata:    raw,
		ID:          candidate.ID,
		WorkspaceID: candidate.WorkspaceID,
	})
	return err
}

func curatorEngineInfo(engine CuratorEngine) CuratorEngineInfo {
	if engine == nil {
		return CuratorEngineInfo{}
	}
	return engine.Info()
}

func validateCuratorDraft(draft CuratorDraft) error {
	required := map[string]string{
		"title":                draft.Title,
		"type":                 draft.Type,
		"problem_pattern":      draft.ProblemPattern,
		"trigger_conditions":   draft.TriggerConditions,
		"diagnostic_steps":     draft.DiagnosticSteps,
		"recommended_practice": draft.RecommendedPractice,
		"applicability":        draft.Applicability,
		"confidence_status":    draft.ConfidenceStatus,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return validationError("curator draft missing " + field)
		}
	}
	if !validKnowledgeType(draft.Type) {
		return validationError("invalid type")
	}
	if !validKnowledgeConfidenceStatus(draft.ConfidenceStatus) {
		return validationError("invalid confidence_status")
	}
	return nil
}

func curatorKnowledgeSources(input CuratorDraftInput) []KnowledgeSourceInput {
	sources := []KnowledgeSourceInput{
		{
			SourceType:    "issue",
			SourceID:      input.Issue.ID,
			SourceTitle:   textValue(issueSourceTitle(input.Issue)),
			SourceExcerpt: textValue(excerpt(issueText(input.Issue), 600)),
		},
	}
	seen := map[string]bool{"issue:" + util.UUIDToString(input.Issue.ID): true}
	add := func(source KnowledgeSourceInput) {
		key := source.SourceType + ":" + util.UUIDToString(source.SourceID) + ":" + source.SourceURL.String
		if seen[key] {
			return
		}
		seen[key] = true
		sources = append(sources, source)
	}
	if input.OriginalItem != nil {
		add(KnowledgeSourceInput{
			SourceType:    "knowledge",
			SourceID:      input.OriginalItem.ID,
			SourceTitle:   textValue(input.OriginalItem.Title),
			SourceExcerpt: textValue(excerpt(input.OriginalItem.ProblemPattern+"\n"+input.OriginalItem.RecommendedPractice, 600)),
		})
	}
	for _, source := range input.OriginalSources {
		add(KnowledgeSourceInput{
			SourceType:    source.SourceType,
			SourceID:      source.SourceID,
			SourceURL:     source.SourceUrl,
			SourceTitle:   source.SourceTitle,
			SourceExcerpt: source.SourceExcerpt,
		})
	}
	if input.TriggerComment != nil {
		add(commentKnowledgeSource(*input.TriggerComment))
	}
	if input.Candidate != nil && input.Candidate.CommentID.Valid {
		for _, comment := range input.Comments {
			if comment.ID == input.Candidate.CommentID {
				add(commentKnowledgeSource(comment))
				break
			}
		}
	}
	if input.TriggerTask != nil {
		add(taskKnowledgeSource(*input.TriggerTask))
	}
	if input.Candidate != nil && input.Candidate.AgentTaskID.Valid {
		for _, task := range input.AgentTasks {
			if task.ID == input.Candidate.AgentTaskID {
				add(taskKnowledgeSource(task))
				break
			}
		}
	}
	for _, feedback := range input.NegativeFeedback {
		if feedback.AgentTaskID.Valid {
			add(KnowledgeSourceInput{
				SourceType:    "agent_task",
				SourceID:      feedback.AgentTaskID,
				SourceTitle:   textValue("Negative feedback task"),
				SourceExcerpt: textValue(feedback.Value + ": " + feedback.Note.String),
			})
		}
	}
	for _, pr := range input.PullRequests {
		add(KnowledgeSourceInput{
			SourceType:    "pull_request",
			SourceID:      pr.ID,
			SourceURL:     textValue(pr.HtmlUrl),
			SourceTitle:   textValue(fmt.Sprintf("%s/%s#%d %s", pr.RepoOwner, pr.RepoName, pr.PrNumber, pr.Title)),
			SourceExcerpt: textValue(fmt.Sprintf("state=%s head_sha=%s additions=%d deletions=%d changed_files=%d", pr.State, pr.HeadSha, pr.Additions, pr.Deletions, pr.ChangedFiles)),
		})
	}
	return sources
}

func firstIssueSourceID(sources []db.KnowledgeSource) pgtype.UUID {
	for _, source := range sources {
		if source.SourceType == "issue" && source.SourceID.Valid {
			return source.SourceID
		}
	}
	return pgtype.UUID{}
}

func governanceFindingSummary(finding db.KnowledgeGovernanceFinding, item db.KnowledgeItem, feedback []db.KnowledgeFeedback) string {
	parts := []string{
		fmt.Sprintf("Governance finding: type=%s status=%s severity=%d reason=%s", finding.FindingType, finding.Status, finding.Severity, finding.Reason),
		"Original knowledge: " + item.Title,
		"Suggested action: " + finding.SuggestedAction,
	}
	for _, row := range feedback {
		note := strings.TrimSpace(row.Note.String)
		if note == "" {
			note = row.Value
		}
		parts = append(parts, "Negative feedback: "+excerpt(note, 800))
	}
	return strings.Join(parts, "\n")
}

func commentKnowledgeSource(comment db.Comment) KnowledgeSourceInput {
	return KnowledgeSourceInput{
		SourceType:    "comment",
		SourceID:      comment.ID,
		SourceTitle:   textValue("Issue comment"),
		SourceExcerpt: textValue(excerpt(comment.Content, 600)),
	}
}

func taskKnowledgeSource(task db.AgentTaskQueue) KnowledgeSourceInput {
	return KnowledgeSourceInput{
		SourceType:    "agent_task",
		SourceID:      task.ID,
		SourceTitle:   textValue("Agent task " + task.Status),
		SourceExcerpt: textValue(excerpt(taskText(task), 600)),
	}
}

func candidateMetadata(candidate db.KnowledgeCandidate) map[string]any {
	meta := map[string]any{}
	if len(candidate.Metadata) > 0 {
		_ = json.Unmarshal(candidate.Metadata, &meta)
	}
	return meta
}

func candidateKnowledgeItemID(candidate db.KnowledgeCandidate) (pgtype.UUID, bool) {
	meta := candidateMetadata(candidate)
	raw, ok := meta["knowledge_item_id"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(raw)
	return id, err == nil
}

func prioritizeKnowledgeComments(comments []db.Comment, triggerID pgtype.UUID) []db.Comment {
	out := append([]db.Comment(nil), comments...)
	sort.SliceStable(out, func(i, j int) bool {
		return commentPriority(out[i], triggerID) < commentPriority(out[j], triggerID)
	})
	return out
}

func commentPriority(comment db.Comment, triggerID pgtype.UUID) int {
	if triggerID.Valid && comment.ID == triggerID {
		return 0
	}
	if comment.AuthorType == "member" && looksLikeUserCorrection(comment.Content) {
		return 1
	}
	if comment.AuthorType == "agent" {
		return 2
	}
	return 3
}

func deterministicSourceSummary(bundle CuratorSourceBundle) string {
	parts := []string{
		fmt.Sprintf("Issue #%d %q is %s/%s.", bundle.Issue.Number, bundle.Issue.Title, bundle.Issue.Status, bundle.Issue.Priority),
		fmt.Sprintf("Collected %d comments, %d agent tasks, and %d pull requests.", len(bundle.Comments), len(bundle.AgentTasks), len(bundle.PullRequests)),
	}
	if bundle.Project != nil {
		parts = append(parts, "Project: "+bundle.Project.Title+".")
	}
	if len(bundle.Labels) > 0 {
		names := make([]string, 0, len(bundle.Labels))
		for _, label := range bundle.Labels {
			names = append(names, label.Name)
		}
		parts = append(parts, "Labels: "+strings.Join(names, ", ")+".")
	}
	return strings.Join(parts, " ")
}

func issueSourceTitle(issue db.Issue) string {
	if issue.Number > 0 {
		return fmt.Sprintf("#%d %s", issue.Number, issue.Title)
	}
	return issue.Title
}

func issueText(issue db.Issue) string {
	if issue.Description.Valid {
		return strings.TrimSpace(issue.Title + "\n" + issue.Description.String)
	}
	return issue.Title
}

func taskText(task db.AgentTaskQueue) string {
	parts := []string{task.Status}
	if task.FailureReason.Valid {
		parts = append(parts, task.FailureReason.String)
	}
	if task.Error.Valid {
		parts = append(parts, task.Error.String)
	}
	if len(task.Result) > 0 {
		parts = append(parts, extractTaskOutput(task.Result, nil))
	}
	return strings.Join(parts, "\n")
}

func textValue(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func excerpt(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return strings.TrimSpace(value[:max]) + "..."
}
