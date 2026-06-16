package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const KnowledgeEmbeddingDimensions = 1536

var knowledgeSlugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

var (
	ErrKnowledgeValidation = errors.New("knowledge validation failed")
	ErrKnowledgeNotFound   = errors.New("knowledge item not found")
)

type KnowledgeService struct {
	Queries   *db.Queries
	TxStarter TxStarter
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
	Query       string
	Embedding   []float32
	Limit       int32
	Filters     KnowledgeSearchFilters
}

type KnowledgeSearchResult struct {
	Item        db.KnowledgeItem
	TextScore   float64
	VectorScore float64
	FinalScore  float64
	MatchReason string
}

type KnowledgeDetail struct {
	Item            db.KnowledgeItem
	Sources         []db.KnowledgeSource
	SourceSummary   KnowledgeSourceSummary
	PublishTargets  []db.KnowledgePublishTarget
	Embeddings      []db.ListKnowledgeEmbeddingMetadataRow
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
	Value           string
	Note            pgtype.Text
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
	feedback, err := s.Queries.GetKnowledgeFeedbackSummary(ctx, db.GetKnowledgeFeedbackSummaryParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	targets, err := s.Queries.ListKnowledgePublishTargets(ctx, db.ListKnowledgePublishTargetsParams{KnowledgeItemID: itemID, WorkspaceID: workspaceID})
	if err != nil {
		return KnowledgeDetail{}, err
	}
	return KnowledgeDetail{Item: item, Sources: sources, SourceSummary: summarizeKnowledgeSources(sources), PublishTargets: targets, Embeddings: embeddings, FeedbackSummary: feedback}, nil
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
			return KnowledgeDetail{}, knowledgePublishTargetErr(err, "wiki page not found")
		}
	} else {
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
			return KnowledgeDetail{}, knowledgePublishTargetErr(err, "skill not found")
		}
		if err := qtx.DeleteSkillFilesBySkill(ctx, skill.ID); err != nil {
			return KnowledgeDetail{}, err
		}
	} else {
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
	if len(embedding) != KnowledgeEmbeddingDimensions {
		return db.UpsertKnowledgeEmbeddingRow{}, validationError(fmt.Sprintf("embedding must have %d dimensions", KnowledgeEmbeddingDimensions))
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
	return s.Queries.UpsertKnowledgeEmbedding(ctx, db.UpsertKnowledgeEmbeddingParams{
		KnowledgeItemID: itemID,
		WorkspaceID:     workspaceID,
		Provider:        strings.TrimSpace(provider),
		Model:           strings.TrimSpace(model),
		ContentHash:     strings.TrimSpace(contentHash),
		Embedding:       pgvector.NewVector(embedding),
	})
}

func (s *KnowledgeService) Search(ctx context.Context, p KnowledgeSearchParams) ([]KnowledgeSearchResult, error) {
	query := strings.TrimSpace(p.Query)
	if p.Limit <= 0 {
		p.Limit = 10
	}
	if p.Limit > 50 {
		p.Limit = 50
	}
	if query == "" && len(p.Embedding) == 0 {
		return nil, validationError("query or embedding is required")
	}
	if len(p.Embedding) > 0 && len(p.Embedding) != KnowledgeEmbeddingDimensions {
		return nil, validationError(fmt.Sprintf("embedding must have %d dimensions", KnowledgeEmbeddingDimensions))
	}
	if err := validateSearchFilters(ctx, s.Queries, p.WorkspaceID, p.Filters); err != nil {
		return nil, err
	}
	if err := validateFilterEnums(p.Filters); err != nil {
		return nil, err
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
			return nil, err
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
		rows, err := s.Queries.SearchKnowledgeVector(ctx, db.SearchKnowledgeVectorParams{
			Embedding:   pgvector.NewVector(p.Embedding),
			WorkspaceID: p.WorkspaceID,
			Types:       p.Filters.Types,
			Statuses:    p.Filters.Statuses,
			ProjectID:   p.Filters.ProjectID,
			AgentID:     p.Filters.AgentID,
			Labels:      normalizeLabels(p.Filters.Labels),
			Limit:       p.Limit,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			key := util.UUIDToString(row.ID)
			result, ok := resultMap[key]
			if !ok {
				item := knowledgeItemFromVectorRow(row)
				result = &KnowledgeSearchResult{Item: item, MatchReason: "vector"}
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
	if _, err := s.recordRetrieval(ctx, p, query, results); err != nil {
		return nil, err
	}
	return results, nil
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
	return s.Queries.CreateKnowledgeFeedback(ctx, db.CreateKnowledgeFeedbackParams{
		KnowledgeItemID: p.KnowledgeItemID,
		WorkspaceID:     p.WorkspaceID,
		MemberID:        p.MemberID,
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

	signals, score, strength, status := s.scoreCandidate(ctx, p, src)
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
	})
}

func (s *KnowledgeService) EvaluateIssueDoneCandidate(ctx context.Context, issue db.Issue) (db.KnowledgeCandidate, error) {
	if issue.Status != "done" {
		return db.KnowledgeCandidate{}, validationError("issue is not done")
	}
	return s.EvaluateCandidate(ctx, KnowledgeCandidateEvaluateParams{
		WorkspaceID:   issue.WorkspaceID,
		SourceType:    "issue",
		SourceID:      issue.ID,
		TriggerReason: "issue_done",
		Issue:         &issue,
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

func (s *KnowledgeService) scoreCandidate(ctx context.Context, p KnowledgeCandidateEvaluateParams, src candidateSource) ([]string, int32, string, string) {
	signals := []string{}
	score := int32(0)
	if p.Manual || p.TriggerReason == "manual" {
		return []string{"manual_mark"}, 100, "manual", "accepted"
	}

	text := src.Issue.Title
	if src.Issue.Description.Valid {
		text += "\n" + src.Issue.Description.String
	}
	if p.AgentTask != nil {
		text += "\n" + extractTaskOutput(p.TaskResult, p.AgentTask.Result)
		if p.AgentTask.ParentTaskID.Valid || p.AgentTask.Attempt > 1 {
			signals = append(signals, "retry_success")
			score += 75
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
				return []string{"no_agent_task"}, 0, "none", "rejected"
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
			for _, comment := range comments {
				text += "\n" + comment.Content
				if comment.AuthorType == "member" && looksLikeUserCorrection(comment.Content) {
					signals = append(signals, "user_correction")
					score += 35
					break
				}
			}
		}
	}

	if looksReusableKnowledge(text) {
		signals = append(signals, "reusable_debug_context")
		score += 30
	}
	signals = uniqueSignals(signals)
	if score > 100 {
		score = 100
	}
	if score >= 80 {
		return signals, score, "strong", "accepted"
	}
	if score >= 50 {
		return signals, score, "weak", "pending"
	}
	if len(signals) == 0 {
		signals = []string{"no_reusable_signal"}
	}
	return signals, score, "none", "rejected"
}

func (s *KnowledgeService) taskWorkspaceID(ctx context.Context, task db.AgentTaskQueue) (pgtype.UUID, error) {
	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return agent.WorkspaceID, nil
}

func (s *KnowledgeService) recordRetrieval(ctx context.Context, p KnowledgeSearchParams, query string, results []KnowledgeSearchResult) (db.KnowledgeRetrievalEvent, error) {
	mode := "text"
	if query == "" {
		mode = "vector"
	} else if len(p.Embedding) > 0 {
		mode = "hybrid"
	}
	topIDs := make([]pgtype.UUID, 0, len(results))
	for _, result := range results {
		topIDs = append(topIDs, result.Item.ID)
	}
	filters, err := json.Marshal(p.Filters)
	if err != nil {
		return db.KnowledgeRetrievalEvent{}, err
	}
	var queryText pgtype.Text
	if query != "" {
		queryText = pgtype.Text{String: query, Valid: true}
	}
	return s.Queries.CreateKnowledgeRetrievalEvent(ctx, db.CreateKnowledgeRetrievalEventParams{
		WorkspaceID:         p.WorkspaceID,
		MemberID:            p.MemberID,
		Query:               queryText,
		RetrievalMode:       mode,
		Filters:             filters,
		ResultCount:         int32(len(results)),
		TopKnowledgeItemIds: topIDs,
	})
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
	case "issue", "comment", "agent_task", "pull_request", "commit":
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
	}
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
