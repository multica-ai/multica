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

// ReviewService handles nightly review draft generation and confirmation.
type ReviewService struct {
	q   *db.Queries
	llm *llm.LLMClient
}

// NewReviewService creates a ReviewService wired to the given queries and LLM client.
func NewReviewService(q *db.Queries, llmClient *llm.LLMClient) *ReviewService {
	return &ReviewService{q: q, llm: llmClient}
}

// GenerateReviewDraft fetches today's time entries and assigned issues, generates a
// Markdown review via LLM (or template fallback), and upserts it as a draft.
// generatedBy should be "manual" (user-triggered) or "scheduled" (nightly job).
func (s *ReviewService) GenerateReviewDraft(ctx context.Context, workspaceID, userID pgtype.UUID, date time.Time, generatedBy string) (db.DailyReview, error) {
	// Look up the member record to use as assignee filter for issues.
	member, err := s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.DailyReview{}, fmt.Errorf("load member: %w", err)
	}

	// Fetch today's time entries (start of day to end of day in UTC).
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)
	entries, err := s.q.ListTimeEntriesByUserRange(ctx, db.ListTimeEntriesByUserRangeParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		StartTime:   pgtype.Timestamptz{Time: dayStart, Valid: true},
		StartTime_2: pgtype.Timestamptz{Time: dayEnd, Valid: true},
	})
	if err != nil {
		slog.Warn("review: failed to list time entries", "error", err)
		entries = nil
	}

	// Fetch issues completed today.
	completedIssues, err := s.q.ListRecentlyCompletedIssuesForMember(ctx, db.ListRecentlyCompletedIssuesForMemberParams{
		WorkspaceID: workspaceID,
		AssigneeID:  member.ID,
		UpdatedAt:   pgtype.Timestamptz{Time: dayStart, Valid: true},
	})
	if err != nil {
		slog.Warn("review: failed to list completed issues", "error", err)
		completedIssues = nil
	}

	// Fetch still-open issues assigned to this member.
	openIssues, err := s.q.ListOpenIssuesForMember(ctx, db.ListOpenIssuesForMemberParams{
		WorkspaceID: workspaceID,
		AssigneeID:  member.ID,
	})
	if err != nil {
		slog.Warn("review: failed to list open issues", "error", err)
		openIssues = nil
	}

	// Generate draft content via LLM, falling back to a structured template.
	content := s.generateContent(ctx, entries, completedIssues, openIssues, date)

	reviewDate := pgtype.Date{Time: dayStart, Valid: true}
	review, err := s.q.UpsertDailyReview(ctx, db.UpsertDailyReviewParams{
		WorkspaceID:  workspaceID,
		UserID:       userID,
		ReviewDate:   reviewDate,
		DraftContent: content,
		GeneratedBy:  generatedBy,
	})
	if err != nil {
		return db.DailyReview{}, fmt.Errorf("upsert review: %w", err)
	}

	slog.Info("review draft generated", "workspace", util.UUIDToString(workspaceID), "user", util.UUIDToString(userID), "date", date.Format("2006-01-02"))
	return review, nil
}

// ConfirmReview marks a review draft as confirmed by the user.
func (s *ReviewService) ConfirmReview(ctx context.Context, workspaceID, reviewID pgtype.UUID) (db.DailyReview, error) {
	review, err := s.q.ConfirmDailyReview(ctx, db.ConfirmDailyReviewParams{
		ID:          reviewID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.DailyReview{}, fmt.Errorf("confirm review: %w", err)
	}
	return review, nil
}

// GetTodayReview returns the review for today's date, or an error if none exists.
func (s *ReviewService) GetTodayReview(ctx context.Context, workspaceID, userID pgtype.UUID) (db.DailyReview, error) {
	today := time.Now().UTC()
	dayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	return s.q.GetDailyReviewByDate(ctx, db.GetDailyReviewByDateParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		ReviewDate:  pgtype.Date{Time: dayStart, Valid: true},
	})
}

// ListReviews returns the most recent reviews for the user (up to limit entries).
func (s *ReviewService) ListReviews(ctx context.Context, workspaceID, userID pgtype.UUID, limit int32) ([]db.DailyReview, error) {
	return s.q.ListDailyReviews(ctx, db.ListDailyReviewsParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Limit:       limit,
	})
}

// generateContent tries LLM generation and falls back to a structured template.
func (s *ReviewService) generateContent(ctx context.Context, entries []db.TimeEntry, completed, open []db.Issue, date time.Time) string {
	prompt := s.buildReviewPrompt(entries, completed, open, date)

	if s.llm.IsConfigured() {
		content, err := s.llm.Generate(ctx, prompt)
		if err == nil {
			return content
		}
		slog.Warn("review: LLM generation failed, using template", "error", err)
	}

	return s.templateReview(entries, completed, open, date)
}

// buildReviewPrompt constructs the LLM prompt from context data.
func (s *ReviewService) buildReviewPrompt(entries []db.TimeEntry, completed, open []db.Issue, date time.Time) string {
	var sb strings.Builder
	sb.WriteString("You are a personal productivity assistant. Generate a nightly review in Chinese markdown.\n\n")
	sb.WriteString(fmt.Sprintf("Today: %s\n\n", date.Format("2006-01-02")))

	sb.WriteString("Time entries:\n")
	if len(entries) == 0 {
		sb.WriteString("- No time entries recorded today.\n")
	} else {
		totalSec := int64(0)
		for _, e := range entries {
			totalSec += e.DurationSeconds
			desc := "(no description)"
			if e.Description.Valid {
				desc = e.Description.String
			}
			sb.WriteString(fmt.Sprintf("- %s: %s (%s)\n",
				desc,
				formatSeconds(e.DurationSeconds),
				e.StartTime.Time.Format("15:04"),
			))
		}
		sb.WriteString(fmt.Sprintf("Total tracked: %s\n", formatSeconds(totalSec)))
	}
	sb.WriteString("\n")

	sb.WriteString("Assigned issues completed today:\n")
	if len(completed) == 0 {
		sb.WriteString("- None\n")
	} else {
		for _, i := range completed {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", i.Status, i.Title))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("Assigned issues still open:\n")
	if len(open) == 0 {
		sb.WriteString("- None\n")
	} else {
		for _, i := range open {
			sb.WriteString(fmt.Sprintf("- [%s][%s] %s\n", i.Priority, i.Status, i.Title))
		}
	}
	sb.WriteString("\n")

	sb.WriteString(`Sections to include:
## 今日完成
## 时间分布 (top 3 time blocks)
## 遗留问题
## 简短反思 (1-2 sentences)

Keep it under 400 words. Be concrete, not generic.`)

	return sb.String()
}

// templateReview produces a structured Markdown review without LLM.
func (s *ReviewService) templateReview(entries []db.TimeEntry, completed, open []db.Issue, date time.Time) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Daily Review - %s\n\n", date.Format("2006-01-02")))

	sb.WriteString("### 今日完成\n")
	if len(completed) == 0 {
		sb.WriteString("- No completed issues recorded.\n")
	} else {
		for _, i := range completed {
			sb.WriteString(fmt.Sprintf("- %s\n", i.Title))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("### 时间分布\n")
	if len(entries) == 0 {
		sb.WriteString("- No time entries recorded today.\n")
	} else {
		totalSec := int64(0)
		for _, e := range entries {
			totalSec += e.DurationSeconds
			desc := "(no description)"
			if e.Description.Valid {
				desc = e.Description.String
			}
			sb.WriteString(fmt.Sprintf("- %s: %s\n", desc, formatSeconds(e.DurationSeconds)))
		}
		sb.WriteString(fmt.Sprintf("\nTotal: %s\n", formatSeconds(totalSec)))
	}
	sb.WriteString("\n")

	sb.WriteString("### 遗留问题\n")
	if len(open) == 0 {
		sb.WriteString("- No open issues.\n")
	} else {
		for _, i := range open {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", i.Priority, i.Title))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("### 简短反思\n")
	sb.WriteString("*(This is a template review. Configure ANTHROPIC_API_KEY for AI-generated reviews.)*\n")

	return sb.String()
}

// formatSeconds converts a duration in seconds to a human-readable "Xh Ym" string.
func formatSeconds(sec int64) string {
	if sec <= 0 {
		return "0m"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
