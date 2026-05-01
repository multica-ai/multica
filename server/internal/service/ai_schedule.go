package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/llm/prompts"
)

// ScheduleSuggestion is a date recommendation for one issue.
type ScheduleSuggestion struct {
	IssueID   string `json:"issue_id"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Reason    string `json:"reason"`
}

// AIScheduleService suggests schedules for issues using an LLM.
type AIScheduleService struct {
	Queries *db.Queries
	Client  llm.LLMClient
}

func NewAIScheduleService(q *db.Queries, client llm.LLMClient) *AIScheduleService {
	return &AIScheduleService{Queries: q, Client: client}
}

// SuggestSchedule fetches issues + dependencies, calls the LLM, and returns scheduling suggestions.
func (s *AIScheduleService) SuggestSchedule(ctx context.Context, workspaceID string, issueIDs []string) ([]ScheduleSuggestion, error) {
	wsUUID := util.ParseUUID(workspaceID)

	// Build a set of requested IDs for quick lookup.
	requestedSet := make(map[string]bool, len(issueIDs))
	for _, id := range issueIDs {
		requestedSet[id] = true
	}

	// Fetch each issue and its dependencies.
	type issueWithDeps struct {
		issue db.Issue
		deps  []db.IssueDependency
	}
	issueMap := make(map[string]issueWithDeps, len(issueIDs))

	for _, id := range issueIDs {
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          util.ParseUUID(id),
			WorkspaceID: wsUUID,
		})
		if err != nil {
			return nil, fmt.Errorf("ai_schedule: get issue %s: %w", id, err)
		}
		deps, err := s.Queries.ListIssueDependenciesForIssue(ctx, issue.ID)
		if err != nil {
			deps = nil
		}
		issueMap[id] = issueWithDeps{issue: issue, deps: deps}
	}

	// Build prompt inputs.
	promptIssues := make([]prompts.ScheduleIssueInput, 0, len(issueIDs))
	for _, id := range issueIDs {
		iwd := issueMap[id]
		iss := iwd.issue

		input := prompts.ScheduleIssueInput{
			ID:       util.UUIDToString(iss.ID),
			Title:    iss.Title,
			Priority: iss.Priority,
			Status:   iss.Status,
		}
		if iss.StartDate.Valid {
			t := iss.StartDate.Time
			input.StartDate = &t
		}
		if iss.EndDate.Valid {
			t := iss.EndDate.Time
			input.EndDate = &t
		}

		// Collect blocked_by IDs (issues that block this one).
		for _, dep := range iwd.deps {
			if dep.Type == "blocks" && util.UUIDToString(dep.IssueID) == id {
				// dep.IssueID blocks dep.DependsOnIssueID → DependsOnIssueID must finish first
				input.BlockedBy = append(input.BlockedBy, util.UUIDToString(dep.DependsOnIssueID))
			}
		}

		promptIssues = append(promptIssues, input)
	}

	system, user := prompts.BuildScheduleMessages(promptIssues, time.Now().UTC())

	resp, err := s.Client.Complete(ctx, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ResponseFormat: "json_object",
		Temperature:    0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("ai_schedule: llm: %w", err)
	}

	var raw struct {
		Suggestions []struct {
			IssueID   string `json:"issue_id"`
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
			Reason    string `json:"reason"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		return nil, fmt.Errorf("ai_schedule: parse response: %w", err)
	}

	suggestions := make([]ScheduleSuggestion, 0, len(raw.Suggestions))
	for _, s := range raw.Suggestions {
		suggestions = append(suggestions, ScheduleSuggestion{
			IssueID:   s.IssueID,
			StartDate: s.StartDate,
			EndDate:   s.EndDate,
			Reason:    s.Reason,
		})
	}
	return suggestions, nil
}
