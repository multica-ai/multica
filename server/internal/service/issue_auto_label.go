package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	WorkspaceSettingAutoLabelNewIssues = "auto_label_new_issues"
	WorkspaceSettingAutoLabelAgentID   = "auto_label_agent_id"
	maxAutoLabelsPerIssue              = 2
	minAutoLabelConfidence             = 0.65
)

// IssueLabelRecommendation is a recommender output that the service can
// resolve to an existing label or create as a new workspace label.
type IssueLabelRecommendation struct {
	Name       string
	Confidence float64
}

// IssueLabelRecommender keeps the auto-labeling policy swappable without
// coupling issue creation to a specific model or heuristic.
type IssueLabelRecommender interface {
	RecommendIssueLabels(ctx context.Context, issue db.Issue, existing []db.IssueLabel) ([]IssueLabelRecommendation, error)
}

// IssueAutoLabelService handles labels for newly created member- or
// agent-authored issues. In production it enqueues an internal Multica agent
// task so the agent can classify via LLM judgment; the recommender path remains
// as a deterministic fallback for tests and deployments without TaskService.
type IssueAutoLabelService struct {
	Queries     *db.Queries
	Bus         *events.Bus
	TaskService *TaskService
	Recommender IssueLabelRecommender
}

func NewIssueAutoLabelService(q *db.Queries, bus *events.Bus, recommender IssueLabelRecommender) *IssueAutoLabelService {
	if recommender == nil {
		recommender = NewKeywordIssueLabelRecommender()
	}
	return &IssueAutoLabelService{
		Queries:     q,
		Bus:         bus,
		Recommender: recommender,
	}
}

func AutoLabelNewIssuesEnabled(settings []byte) bool {
	if len(settings) == 0 {
		return false
	}
	var parsed map[string]any
	if err := json.Unmarshal(settings, &parsed); err != nil {
		return false
	}
	return parsed[WorkspaceSettingAutoLabelNewIssues] == true
}

func AutoLabelEligibleCreatorType(creatorType string) bool {
	return creatorType == "member" || creatorType == "agent"
}

func AutoLabelAgentID(settings []byte) string {
	if len(settings) == 0 {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal(settings, &parsed); err != nil {
		return ""
	}
	if id, ok := parsed[WorkspaceSettingAutoLabelAgentID].(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}

func (s *IssueAutoLabelService) AutoLabelCreatedIssue(ctx context.Context, issueID string) error {
	if s == nil || s.Queries == nil || s.Recommender == nil {
		return nil
	}
	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		return err
	}
	issue, err := s.Queries.GetIssue(ctx, issueUUID)
	if err != nil {
		return err
	}
	if !AutoLabelEligibleCreatorType(issue.CreatorType) {
		return nil
	}
	workspace, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return err
	}
	if !AutoLabelNewIssuesEnabled(workspace.Settings) {
		return nil
	}
	current, err := s.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return err
	}
	if len(current) > 0 {
		return nil
	}
	if s.TaskService != nil {
		return s.enqueueAgentAutoLabelTask(ctx, issue, workspace.Settings)
	}

	return s.applyRecommendedLabels(ctx, issue)
}

func (s *IssueAutoLabelService) enqueueAgentAutoLabelTask(ctx context.Context, issue db.Issue, settings []byte) error {
	agentID, ok, err := s.selectAutoLabelAgent(ctx, issue, settings)
	if err != nil {
		return err
	}
	if !ok {
		slog.Debug("issue auto-label: no ready agent available",
			"issue_id", util.UUIDToString(issue.ID),
			"workspace_id", util.UUIDToString(issue.WorkspaceID),
		)
		return nil
	}
	_, err = s.TaskService.EnqueueIssueAutoLabelTask(ctx, issue, agentID)
	return err
}

func (s *IssueAutoLabelService) selectAutoLabelAgent(ctx context.Context, issue db.Issue, settings []byte) (pgtype.UUID, bool, error) {
	if configuredID := AutoLabelAgentID(settings); configuredID != "" {
		agentID, err := util.ParseUUID(configuredID)
		if err != nil {
			slog.Warn("issue auto-label: configured agent id is invalid",
				"issue_id", util.UUIDToString(issue.ID),
				"agent_id", configuredID,
				"error", err,
			)
		} else if s.isReadyWorkspaceAgent(ctx, agentID, issue.WorkspaceID) {
			return agentID, true, nil
		}
	}
	if issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid {
		if s.isReadyWorkspaceAgent(ctx, issue.AssigneeID, issue.WorkspaceID) {
			return issue.AssigneeID, true, nil
		}
	}
	if issue.CreatorType == "agent" && issue.CreatorID.Valid {
		if s.isReadyWorkspaceAgent(ctx, issue.CreatorID, issue.WorkspaceID) {
			return issue.CreatorID, true, nil
		}
	}
	agents, err := s.Queries.ListAgents(ctx, issue.WorkspaceID)
	if err != nil {
		return pgtype.UUID{}, false, err
	}
	for _, agent := range agents {
		if agent.RuntimeID.Valid && !agent.ArchivedAt.Valid {
			return agent.ID, true, nil
		}
	}
	return pgtype.UUID{}, false, nil
}

func (s *IssueAutoLabelService) isReadyWorkspaceAgent(ctx context.Context, agentID, workspaceID pgtype.UUID) bool {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return false
	}
	return agent.WorkspaceID == workspaceID && agent.RuntimeID.Valid && !agent.ArchivedAt.Valid
}

func (s *IssueAutoLabelService) applyRecommendedLabels(ctx context.Context, issue db.Issue) error {
	existing, err := s.Queries.ListLabels(ctx, issue.WorkspaceID)
	if err != nil {
		return err
	}
	recommendations, err := s.Recommender.RecommendIssueLabels(ctx, issue, existing)
	if err != nil {
		return err
	}

	attached := 0
	for _, rec := range recommendations {
		if attached >= maxAutoLabelsPerIssue {
			break
		}
		if rec.Confidence < minAutoLabelConfidence {
			continue
		}
		label, created, err := s.ensureLabel(ctx, issue.WorkspaceID, rec.Name, existing)
		if err != nil {
			slog.Warn("issue auto-label: failed to ensure label",
				"issue_id", util.UUIDToString(issue.ID),
				"label", rec.Name,
				"error", err,
			)
			continue
		}
		if created {
			existing = append(existing, label)
			s.publishLabelCreated(issue.WorkspaceID, label)
		}
		if err := s.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
			IssueID:     issue.ID,
			LabelID:     label.ID,
			WorkspaceID: issue.WorkspaceID,
		}); err != nil {
			slog.Warn("issue auto-label: failed to attach label",
				"issue_id", util.UUIDToString(issue.ID),
				"label_id", util.UUIDToString(label.ID),
				"error", err,
			)
			continue
		}
		attached++
	}
	if attached == 0 {
		return nil
	}

	labels, err := s.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return err
	}
	s.publishIssueLabelsChanged(issue.WorkspaceID, issue.ID, labels)
	return nil
}

func (s *IssueAutoLabelService) ensureLabel(ctx context.Context, workspaceID pgtype.UUID, rawName string, existing []db.IssueLabel) (db.IssueLabel, bool, error) {
	name, err := normalizeAutoLabelName(rawName)
	if err != nil {
		return db.IssueLabel{}, false, err
	}
	normalized := strings.ToLower(name)
	for _, label := range existing {
		if strings.ToLower(label.Name) == normalized {
			return label, false, nil
		}
	}
	label, err := s.Queries.CreateLabel(ctx, db.CreateLabelParams{
		WorkspaceID: workspaceID,
		Name:        name,
		Color:       colorForAutoLabel(name),
	})
	if err == nil {
		return label, true, nil
	}
	if !isAutoLabelUniqueViolation(err) {
		return db.IssueLabel{}, false, err
	}
	label, err = s.Queries.GetLabelByName(ctx, db.GetLabelByNameParams{
		WorkspaceID: workspaceID,
		Name:        name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.IssueLabel{}, false, err
		}
		return db.IssueLabel{}, false, err
	}
	return label, false, nil
}

func normalizeAutoLabelName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("label name is required")
	}
	if len(name) > 32 {
		return "", errors.New("label name must be 32 characters or fewer")
	}
	return name, nil
}

func isAutoLabelUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *IssueAutoLabelService) publishLabelCreated(workspaceID pgtype.UUID, label db.IssueLabel) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventLabelCreated,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"label": labelEventPayload(label),
		},
	})
}

func (s *IssueAutoLabelService) publishIssueLabelsChanged(workspaceID, issueID pgtype.UUID, labels []db.IssueLabel) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueLabelsChanged,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"issue_id": util.UUIDToString(issueID),
			"labels":   labelListEventPayload(labels),
		},
	})
}

func labelListEventPayload(labels []db.IssueLabel) []map[string]any {
	out := make([]map[string]any, len(labels))
	for i, label := range labels {
		out[i] = labelEventPayload(label)
	}
	return out
}

func labelEventPayload(label db.IssueLabel) map[string]any {
	return map[string]any{
		"id":           util.UUIDToString(label.ID),
		"workspace_id": util.UUIDToString(label.WorkspaceID),
		"name":         label.Name,
		"color":        label.Color,
		"created_at":   util.TimestampToString(label.CreatedAt),
		"updated_at":   util.TimestampToString(label.UpdatedAt),
	}
}

type keywordLabelCategory struct {
	Name     string
	Keywords []string
}

type KeywordIssueLabelRecommender struct {
	categories []keywordLabelCategory
}

func NewKeywordIssueLabelRecommender() *KeywordIssueLabelRecommender {
	return &KeywordIssueLabelRecommender{categories: defaultKeywordLabelCategories()}
}

func (r *KeywordIssueLabelRecommender) RecommendIssueLabels(_ context.Context, issue db.Issue, _ []db.IssueLabel) ([]IssueLabelRecommendation, error) {
	if r == nil {
		r = NewKeywordIssueLabelRecommender()
	}
	title := strings.ToLower(issue.Title)
	description := ""
	if issue.Description.Valid {
		description = strings.ToLower(issue.Description.String)
	}

	type scoredRecommendation struct {
		IssueLabelRecommendation
		score int
	}
	scored := make([]scoredRecommendation, 0, len(r.categories))
	for _, category := range r.categories {
		score := 0
		for _, keyword := range category.Keywords {
			needle := strings.ToLower(keyword)
			score += strings.Count(title, needle) * 2
			score += strings.Count(description, needle)
		}
		if score == 0 {
			continue
		}
		confidence := math.Min(0.95, 0.55+(float64(score)*0.15))
		scored = append(scored, scoredRecommendation{
			IssueLabelRecommendation: IssueLabelRecommendation{
				Name:       category.Name,
				Confidence: confidence,
			},
			score: score,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].Name < scored[j].Name
		}
		return scored[i].score > scored[j].score
	})
	out := make([]IssueLabelRecommendation, 0, maxAutoLabelsPerIssue)
	for _, rec := range scored {
		out = append(out, rec.IssueLabelRecommendation)
		if len(out) >= maxAutoLabelsPerIssue {
			break
		}
	}
	return out, nil
}

func defaultKeywordLabelCategories() []keywordLabelCategory {
	return []keywordLabelCategory{
		{Name: "bug", Keywords: []string{"bug", "error", "crash", "fail", "failed", "failure", "broken", "regression", "fix", "오류", "버그", "실패", "에러", "고장", "报错", "错误"}},
		{Name: "feature", Keywords: []string{"feature", "add", "create", "support", "implement", "new", "기능", "추가", "구현", "만들", "新建", "新增", "支持"}},
		{Name: "docs", Keywords: []string{"docs", "documentation", "readme", "guide", "document", "문서", "기록", "정리", "가이드", "文档", "指南"}},
		{Name: "ui", Keywords: []string{"ui", "ux", "design", "layout", "button", "modal", "screen", "화면", "디자인", "버튼", "界面", "设计"}},
		{Name: "backend", Keywords: []string{"api", "server", "database", "db", "migration", "backend", "sql", "서버", "백엔드", "데이터베이스", "后端", "数据库"}},
		{Name: "frontend", Keywords: []string{"frontend", "react", "component", "view", "web", "프론트", "컴포넌트", "前端", "组件"}},
		{Name: "devops", Keywords: []string{"docker", "image", "deploy", "ci", "workflow", "release", "infra", "compose", "도커", "배포", "이미지", "部署", "镜像"}},
		{Name: "automation", Keywords: []string{"auto", "automatic", "automation", "autopilot", "label", "labels", "자동", "자동화", "라벨", "自动", "标签"}},
	}
}

func colorForAutoLabel(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bug":
		return "#ef4444"
	case "feature":
		return "#3b82f6"
	case "docs":
		return "#6366f1"
	case "ui":
		return "#ec4899"
	case "backend":
		return "#0f766e"
	case "frontend":
		return "#8b5cf6"
	case "devops":
		return "#f59e0b"
	case "automation":
		return "#06b6d4"
	default:
		palette := []string{"#ef4444", "#f97316", "#eab308", "#22c55e", "#06b6d4", "#3b82f6", "#8b5cf6", "#ec4899"}
		hash := 0
		for _, r := range strings.ToLower(name) {
			hash = int(r) + ((hash << 5) - hash)
		}
		if hash < 0 {
			hash = -hash
		}
		return palette[hash%len(palette)]
	}
}
