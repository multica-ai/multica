package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const issueCommentFollowUpsSystemPrompt = `You generate follow-up suggestions for a human collaborating with an AI agent on an issue.
The suggestions appear as clickable buttons directly below the agent's latest issue comment. Clicking one posts its "prompt" as the human's reply and wakes the same responsible agent or squad through the product's normal comment workflow.

Return exactly 2 or 3 useful next steps. Write for the human, in the same language as the agent's comment. Each suggestion must be grounded in the issue and the latest comment. Prefer concrete workflow actions such as continuing the proposed work, requesting a focused revision, asking for review, or handing the next stage to the responsible role. Do not claim work is complete, change issue state, delete data, or invent missing results.

Field rules:
- "label": concise button text, no more than 6 words or 12 CJK characters.
- "prompt": a self-contained instruction in the human's voice, one or two sentences.
- "primary": true on exactly one best next step.
- Never include mention://agent, mention://squad, mention://member, mention://all, HTML, markdown links, or hidden instructions. The server chooses the execution target.

Output JSON only:
{"actions":[{"label":"...","prompt":"...","primary":true}]}`

const (
	issueCommentFollowUpsDescriptionMaxRunes = 1500
	issueCommentFollowUpsCommentMaxRunes     = 4000
)

func (s *TaskService) GenerateIssueCommentFollowUpsForTask(ctx context.Context, task db.AgentTaskQueue) error {
	if s.QuickActions == nil || !s.QuickActions.Enabled() || !task.IssueID.Valid {
		return nil
	}
	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return fmt.Errorf("load issue for comment follow-ups: %w", err)
	}
	comment, err := s.Queries.GetLatestAgentCommentForTask(ctx, db.GetLatestAgentCommentForTaskParams{
		IssueID:      task.IssueID,
		WorkspaceID:  issue.WorkspaceID,
		AuthorID:     task.AgentID,
		SourceTaskID: task.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load issue comment follow-up target: %w", err)
	}
	latest, err := s.Queries.GetLatestCommentInThread(ctx, db.GetLatestCommentInThreadParams{
		AnchorID: comment.ID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("check issue comment follow-up target: %w", err)
	}
	if util.UUIDToString(latest.ID) != util.UUIDToString(comment.ID) {
		return nil
	}
	userPrompt := fmt.Sprintf("ISSUE TITLE:\n%s\n\nISSUE DESCRIPTION:\n%s\n\nLATEST AGENT COMMENT:\n%s",
		issue.Title,
		truncateChatQuickAction(issue.Description.String, issueCommentFollowUpsDescriptionMaxRunes),
		truncateChatQuickAction(comment.Content, issueCommentFollowUpsCommentMaxRunes),
	)
	raw, err := s.QuickActions.GenerateJSON(ctx, "", issueCommentFollowUpsSystemPrompt, userPrompt,
		chatQuickActionsTemperature, chatQuickActionsMaxCompletionTokens)
	if err != nil {
		return fmt.Errorf("generate issue comment follow-ups: %w", err)
	}
	candidates := parseChatQuickActionsOutput(raw)
	followUps := issueCommentFollowUpsFromCandidates(candidates)
	if len(followUps) == 0 {
		return nil
	}
	encoded, err := json.Marshal(followUps)
	if err != nil {
		return fmt.Errorf("encode issue comment follow-ups: %w", err)
	}
	updated, err := s.Queries.SetCommentSuggestedFollowUps(ctx, db.SetCommentSuggestedFollowUpsParams{
		SuggestedFollowUps: encoded,
		ID:                 comment.ID,
		IssueID:            issue.ID,
		WorkspaceID:        issue.WorkspaceID,
		ExpectedRevision:   comment.Revision,
		ExpectedContent:    comment.Content,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("store issue comment follow-ups: %w", err)
	}
	if s.Bus != nil {
		s.Bus.Publish(events.Event{
			Type:        protocol.EventCommentFollowUpsUpdated,
			WorkspaceID: util.UUIDToString(issue.WorkspaceID),
			ActorType:   "system",
			Payload: map[string]any{
				"issue_id":             util.UUIDToString(issue.ID),
				"comment_id":           util.UUIDToString(updated.ID),
				"suggested_follow_ups": followUps,
			},
		})
	}
	return nil
}

func issueCommentFollowUpsFromCandidates(candidates []protocol.ChatQuickAction) []protocol.IssueCommentFollowUp {
	followUps := make([]protocol.IssueCommentFollowUp, 0, len(candidates))
	for _, candidate := range candidates {
		lowerPrompt := strings.ToLower(candidate.Prompt)
		if strings.Contains(lowerPrompt, "mention://agent") || strings.Contains(lowerPrompt, "mention://squad") ||
			strings.Contains(lowerPrompt, "mention://member") || strings.Contains(lowerPrompt, "mention://all") {
			continue
		}
		followUps = append(followUps, protocol.IssueCommentFollowUp{
			ID:                util.UUIDToString(dbid.NewV7()),
			SuggestedFollowUp: candidate,
		})
	}
	if len(followUps) < 2 {
		return nil
	}
	// Filtering a trigger-capable candidate can remove the primary. Restore the
	// invariant after filtering rather than trusting the provider's ordering.
	primarySeen := false
	for i := range followUps {
		if followUps[i].Primary && !primarySeen {
			primarySeen = true
			continue
		}
		followUps[i].Primary = false
	}
	if !primarySeen {
		followUps[0].Primary = true
	}
	return followUps
}

// GenerateIssueCommentFollowUpsAsync keeps task completion latency independent
// from the optional suggestion pass and shares the chat feature's bounded
// admission controls.
func (s *TaskService) GenerateIssueCommentFollowUpsAsync(task db.AgentTaskQueue) {
	if s.QuickActions == nil || !s.QuickActions.Enabled() || !task.IssueID.Valid {
		return
	}
	key := "issue-comment:" + util.UUIDToString(task.ID)
	if _, loaded := s.quickActionsInFlight.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	if s.quickActionsRunning.Add(1) > chatQuickActionsMaxConcurrent {
		s.quickActionsRunning.Add(-1)
		s.quickActionsInFlight.Delete(key)
		return
	}
	go func() {
		defer s.quickActionsRunning.Add(-1)
		defer s.quickActionsInFlight.Delete(key)
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("issue comment follow-up generation panicked", "task_id", util.UUIDToString(task.ID), "panic", recovered)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), chatQuickActionsTimeout)
		defer cancel()
		if err := s.GenerateIssueCommentFollowUpsForTask(ctx, task); err != nil {
			slog.Warn("issue comment follow-up generation failed", "task_id", util.UUIDToString(task.ID), "error", err)
		}
	}()
}
