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

// StandupSummaryResult holds a generated standup summary.
type StandupSummaryResult struct {
	Content     string    `json:"content"`
	Date        time.Time `json:"date"`
	MemberCount int       `json:"member_count"`
}

// StandupService generates team standup summaries from yesterday's activity.
type StandupService struct {
	q   *db.Queries
	llm *llm.LLMClient
}

// NewStandupService creates a StandupService wired to the given queries and LLM client.
func NewStandupService(q *db.Queries, llmClient *llm.LLMClient) *StandupService {
	return &StandupService{q: q, llm: llmClient}
}

// memberEntry groups a member's display name with their yesterday's activity.
type memberEntry struct {
	Name    string
	Entries []db.TimeEntry
	Closed  []db.Issue
}

// GenerateSummary builds a standup summary for the workspace summarising
// yesterday's time entries and completed issues across all active members.
func (s *StandupService) GenerateSummary(ctx context.Context, workspaceID pgtype.UUID) (*StandupSummaryResult, error) {
	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	dayStart := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	members, err := s.q.ListMembersWithUser(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	var entries []memberEntry
	for _, m := range members {
		timeEntries, err := s.q.ListTimeEntriesByUserRange(ctx, db.ListTimeEntriesByUserRangeParams{
			WorkspaceID: workspaceID,
			UserID:      m.UserID,
			StartTime:   pgtype.Timestamptz{Time: dayStart, Valid: true},
			StartTime_2: pgtype.Timestamptz{Time: dayEnd, Valid: true},
		})
		if err != nil {
			slog.Warn("standup: failed to fetch time entries",
				"member_id", util.UUIDToString(m.ID), "error", err)
			timeEntries = nil
		}

		// Fetch member record to get the member ID (assignee_id) for issue queries.
		memberRecord, err := s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      m.UserID,
			WorkspaceID: workspaceID,
		})
		var closedIssues []db.Issue
		if err == nil {
			closedIssues, err = s.q.ListRecentlyCompletedIssuesForMember(ctx, db.ListRecentlyCompletedIssuesForMemberParams{
				WorkspaceID: workspaceID,
				AssigneeID:  memberRecord.ID,
				UpdatedAt:   pgtype.Timestamptz{Time: dayStart, Valid: true},
			})
			if err != nil {
				slog.Warn("standup: failed to fetch completed issues",
					"member_id", util.UUIDToString(m.ID), "error", err)
				closedIssues = nil
			}
		}

		// Skip members who had no activity yesterday.
		if len(timeEntries) == 0 && len(closedIssues) == 0 {
			continue
		}

		entries = append(entries, memberEntry{
			Name:    m.UserName,
			Entries: timeEntries,
			Closed:  closedIssues,
		})
	}

	content := s.generateContent(ctx, entries, dayStart)
	return &StandupSummaryResult{
		Content:     content,
		Date:        dayStart,
		MemberCount: len(entries),
	}, nil
}

// generateContent tries LLM generation; falls back to a structured template.
func (s *StandupService) generateContent(ctx context.Context, members []memberEntry, date time.Time) string {
	prompt := s.buildPrompt(members, date)
	if s.llm.IsConfigured() {
		content, err := s.llm.Generate(ctx, prompt)
		if err == nil {
			return content
		}
		slog.Warn("standup: LLM generation failed, using template fallback", "error", err)
	}
	return s.templateSummary(members, date)
}

// buildPrompt constructs the LLM prompt from member activity data.
func (s *StandupService) buildPrompt(members []memberEntry, date time.Time) string {
	var sb strings.Builder
	sb.WriteString("You are a team assistant. Generate a concise daily standup summary in markdown.\n\n")
	sb.WriteString(fmt.Sprintf("Date: %s (yesterday's activity)\n\n", date.Format("2006-01-02")))

	if len(members) == 0 {
		sb.WriteString("No team members recorded activity yesterday.\n")
	} else {
		for _, m := range members {
			sb.WriteString(fmt.Sprintf("### %s\n", m.Name))
			if len(m.Closed) > 0 {
				sb.WriteString("Completed:\n")
				for _, i := range m.Closed {
					sb.WriteString(fmt.Sprintf("- %s\n", i.Title))
				}
			}
			if len(m.Entries) > 0 {
				total := int64(0)
				for _, e := range m.Entries {
					total += e.DurationSeconds
				}
				sb.WriteString(fmt.Sprintf("Time tracked: %s\n", formatSeconds(total)))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("Format as a standup summary with one section per person. Keep it brief and factual. Under 300 words.")
	return sb.String()
}

// templateSummary produces a structured Markdown summary without LLM.
func (s *StandupService) templateSummary(members []memberEntry, date time.Time) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Daily Standup — %s\n\n", date.Format("2006-01-02")))

	if len(members) == 0 {
		sb.WriteString("No activity recorded by team members yesterday.\n")
		return sb.String()
	}

	for _, m := range members {
		sb.WriteString(fmt.Sprintf("### %s\n", m.Name))
		if len(m.Closed) > 0 {
			sb.WriteString("**Completed:**\n")
			for _, i := range m.Closed {
				sb.WriteString(fmt.Sprintf("- %s\n", i.Title))
			}
		} else {
			sb.WriteString("**Completed:** Nothing closed yesterday.\n")
		}
		if len(m.Entries) > 0 {
			total := int64(0)
			for _, e := range m.Entries {
				total += e.DurationSeconds
			}
			sb.WriteString(fmt.Sprintf("**Time tracked:** %s\n", formatSeconds(total)))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("*(Generated from time entries and issue completions — configure ANTHROPIC_API_KEY for AI-generated summaries.)*\n")
	return sb.String()
}
