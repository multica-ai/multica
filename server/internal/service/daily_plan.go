package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/llm"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DailyPlanService handles next-day plan draft generation and confirmation.
type DailyPlanService struct {
	q   *db.Queries
	llm *llm.LLMClient
}

// NewDailyPlanService creates a DailyPlanService wired to the given queries and LLM client.
func NewDailyPlanService(q *db.Queries, llmClient *llm.LLMClient) *DailyPlanService {
	return &DailyPlanService{q: q, llm: llmClient}
}

// GeneratePlanDraft fetches open issues and yesterday's review, generates a next-day plan
// via LLM (or template fallback), and upserts it as a draft.
// planDate should be tomorrow's date. generatedBy is "manual" or "scheduled".
func (s *DailyPlanService) GeneratePlanDraft(ctx context.Context, workspaceID, userID pgtype.UUID, planDate time.Time, generatedBy string) (db.DailyPlan, error) {
	// Look up the member record to use as assignee filter.
	member, err := s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.DailyPlan{}, fmt.Errorf("load member: %w", err)
	}

	// Fetch open issues assigned to this member.
	openIssues, err := s.q.ListOpenIssuesForMember(ctx, db.ListOpenIssuesForMemberParams{
		WorkspaceID: workspaceID,
		AssigneeID:  member.ID,
	})
	if err != nil {
		slog.Warn("plan: failed to list open issues", "error", err)
		openIssues = nil
	}

	// Fetch yesterday's confirmed review for additional context.
	yesterday := planDate.Add(-48 * time.Hour) // planDate is tomorrow, so -48h = yesterday
	yesterdayStart := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)
	var prevReview *db.DailyReview
	review, err := s.q.GetDailyReviewByDate(ctx, db.GetDailyReviewByDateParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		ReviewDate:  pgtype.Date{Time: yesterdayStart, Valid: true},
	})
	if err == nil && review.Status == "confirmed" {
		prevReview = &review
	}

	// Collect top issue IDs (first 3 by priority order).
	topIssueIDs := make([]pgtype.UUID, 0, 3)
	for i, issue := range openIssues {
		if i >= 3 {
			break
		}
		topIssueIDs = append(topIssueIDs, issue.ID)
	}

	// Generate draft content via LLM, falling back to structured template.
	content := s.generatePlanContent(ctx, openIssues, prevReview, planDate)

	dayStart := time.Date(planDate.Year(), planDate.Month(), planDate.Day(), 0, 0, 0, 0, time.UTC)
	plan, err := s.q.UpsertDailyPlan(ctx, db.UpsertDailyPlanParams{
		WorkspaceID:  workspaceID,
		UserID:       userID,
		PlanDate:     pgtype.Date{Time: dayStart, Valid: true},
		DraftContent: content,
		TopIssueIds:  topIssueIDs,
		GeneratedBy:  generatedBy,
	})
	if err != nil {
		return db.DailyPlan{}, fmt.Errorf("upsert plan: %w", err)
	}

	slog.Info("plan draft generated", "workspace", util.UUIDToString(workspaceID), "user", util.UUIDToString(userID), "plan_date", planDate.Format("2006-01-02"))
	return plan, nil
}

// ConfirmPlan marks a plan draft as confirmed by the user.
func (s *DailyPlanService) ConfirmPlan(ctx context.Context, workspaceID, planID pgtype.UUID) (db.DailyPlan, error) {
	plan, err := s.q.ConfirmDailyPlan(ctx, db.ConfirmDailyPlanParams{
		ID:          planID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.DailyPlan{}, fmt.Errorf("confirm plan: %w", err)
	}
	return plan, nil
}

// GetTomorrowPlan returns the plan for tomorrow's date, or an error if none exists.
func (s *DailyPlanService) GetTomorrowPlan(ctx context.Context, workspaceID, userID pgtype.UUID) (db.DailyPlan, error) {
	tomorrow := time.Now().UTC().Add(24 * time.Hour)
	dayStart := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)
	return s.q.GetDailyPlanByDate(ctx, db.GetDailyPlanByDateParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		PlanDate:    pgtype.Date{Time: dayStart, Valid: true},
	})
}

// ListPlans returns the most recent plans for the user (up to limit entries).
func (s *DailyPlanService) ListPlans(ctx context.Context, workspaceID, userID pgtype.UUID, limit int32) ([]db.DailyPlan, error) {
	return s.q.ListDailyPlans(ctx, db.ListDailyPlansParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Limit:       limit,
	})
}

// generatePlanContent tries LLM generation and falls back to a structured template.
func (s *DailyPlanService) generatePlanContent(ctx context.Context, issues []db.Issue, prevReview *db.DailyReview, planDate time.Time) string {
	prompt := s.buildPlanPrompt(issues, prevReview, planDate)

	if s.llm.IsConfigured() {
		content, err := s.llm.Generate(ctx, prompt)
		if err == nil {
			return content
		}
		slog.Warn("plan: LLM generation failed, using template", "error", err)
	}

	return s.templatePlan(issues, planDate)
}

// buildPlanPrompt constructs the LLM prompt from context data.
func (s *DailyPlanService) buildPlanPrompt(issues []db.Issue, prevReview *db.DailyReview, planDate time.Time) string {
	var sb strings.Builder
	sb.WriteString("You are a personal productivity assistant. Generate a next-day plan in Chinese markdown.\n\n")
	sb.WriteString(fmt.Sprintf("Tomorrow: %s\n\n", planDate.Format("2006-01-02")))

	sb.WriteString("Open issues assigned to me (by priority):\n")
	if len(issues) == 0 {
		sb.WriteString("- No open issues.\n")
	} else {
		for _, i := range issues {
			due := ""
			if i.DueDate.Valid {
				due = fmt.Sprintf(" [due: %s]", i.DueDate.Time.Format("2006-01-02"))
			}
			sb.WriteString(fmt.Sprintf("- [%s][%s]%s %s\n", i.Priority, i.Status, due, i.Title))
		}
	}
	sb.WriteString("\n")

	if prevReview != nil {
		sb.WriteString("Yesterday's review notes:\n")
		// Include only the first 500 chars to keep the prompt concise.
		notes := prevReview.DraftContent
		if len(notes) > 500 {
			notes = notes[:500] + "..."
		}
		sb.WriteString(notes)
		sb.WriteString("\n\n")
	}

	sb.WriteString(`Output:
## 🐸 三只青蛙 (Top 3 most important/hardest — do these first)
## 📋 建议顺序 (numbered list with time estimates)
## ⏰ 预计专注时间

Keep it under 300 words. Be specific about issue titles.`)

	return sb.String()
}

// templatePlan produces a structured Markdown plan without LLM.
func (s *DailyPlanService) templatePlan(issues []db.Issue, planDate time.Time) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Tomorrow's Plan - %s\n\n", planDate.Format("2006-01-02")))

	sb.WriteString("### 🐸 三只青蛙\n")
	if len(issues) == 0 {
		sb.WriteString("1. (No issues assigned)\n2. -\n3. -\n")
	} else {
		for i, issue := range issues {
			if i >= 3 {
				break
			}
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, issue.Title))
		}
		for i := len(issues); i < 3; i++ {
			sb.WriteString(fmt.Sprintf("%d. -\n", i+1))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("### 📋 建议顺序\n")
	if len(issues) == 0 {
		sb.WriteString("- (No issues)\n")
	} else {
		for idx, issue := range issues {
			sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", idx+1, issue.Priority, issue.Title))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("### ⏰ 预计专注时间\n")
	estHours := len(issues) * 2
	sb.WriteString(fmt.Sprintf("~%d hours focused work\n\n", estHours))

	sb.WriteString("*(This is a template plan. Configure ANTHROPIC_API_KEY for AI-generated plans.)*\n")

	return sb.String()
}
