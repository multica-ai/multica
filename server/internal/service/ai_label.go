package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/llm/prompts"
)

// LabelSuggestion is a single label suggestion for an issue.
type LabelSuggestion struct {
	Name     string  `json:"name"`
	Existing bool    `json:"existing"`
	LabelID  *string `json:"label_id,omitempty"`
	Color    *string `json:"color,omitempty"`
}

// IssueLabelResult holds suggestions for one issue.
type IssueLabelResult struct {
	IssueID     string            `json:"issue_id"`
	Suggestions []LabelSuggestion `json:"suggestions"`
}

// AILabelService suggests labels for issues using an LLM.
type AILabelService struct {
	Queries *db.Queries
	Client  llm.LLMClient
}

func NewAILabelService(q *db.Queries, client llm.LLMClient) *AILabelService {
	return &AILabelService{Queries: q, Client: client}
}

// SuggestLabels fetches issue content and workspace labels, calls the LLM, and returns suggestions.
func (s *AILabelService) SuggestLabels(ctx context.Context, workspaceID string, issueIDs []string, rules []string) ([]IssueLabelResult, error) {
	wsUUID := util.ParseUUID(workspaceID)

	// Fetch existing workspace labels for context.
	existingLabels, err := s.Queries.ListLabelsByWorkspace(ctx, wsUUID)
	if err != nil {
		return nil, fmt.Errorf("ai_label: list labels: %w", err)
	}
	promptLabels := make([]prompts.ExistingLabel, len(existingLabels))
	for i, l := range existingLabels {
		promptLabels[i] = prompts.ExistingLabel{
			ID:    util.UUIDToString(l.ID),
			Name:  l.Name,
			Color: l.Color,
		}
	}

	// Fetch each issue.
	var issues []prompts.LabelIssueInput
	for _, id := range issueIDs {
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          util.ParseUUID(id),
			WorkspaceID: wsUUID,
		})
		if err != nil {
			return nil, fmt.Errorf("ai_label: get issue %s: %w", id, err)
		}
		desc := ""
		if issue.Description.Valid {
			desc = issue.Description.String
		}
		issues = append(issues, prompts.LabelIssueInput{
			ID:          util.UUIDToString(issue.ID),
			Title:       issue.Title,
			Description: desc,
		})
	}

	system, user := prompts.BuildLabelMessages(issues, promptLabels, rules)

	resp, err := s.Client.Complete(ctx, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ResponseFormat: "json_object",
		Temperature:    0.2,
	})
	if err != nil {
		return nil, fmt.Errorf("ai_label: llm: %w", err)
	}

	// Parse LLM response.
	var raw struct {
		Results []struct {
			IssueID     string `json:"issue_id"`
			Suggestions []struct {
				Name     string  `json:"name"`
				Existing bool    `json:"existing"`
				LabelID  *string `json:"label_id,omitempty"`
				Color    *string `json:"color,omitempty"`
			} `json:"suggestions"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		return nil, fmt.Errorf("ai_label: parse response: %w", err)
	}

	results := make([]IssueLabelResult, len(raw.Results))
	for i, r := range raw.Results {
		suggestions := make([]LabelSuggestion, len(r.Suggestions))
		for j, s := range r.Suggestions {
			suggestions[j] = LabelSuggestion{
				Name:     s.Name,
				Existing: s.Existing,
				LabelID:  s.LabelID,
				Color:    s.Color,
			}
		}
		results[i] = IssueLabelResult{
			IssueID:     r.IssueID,
			Suggestions: suggestions,
		}
	}
	return results, nil
}
