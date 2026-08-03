// Package commentguard adapts issue comments to before.message.send.
// Workflow policies own every decision; this package only collects facts and
// applies the returned decision.
package commentguard

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
)

type Service struct {
	evaluator workflows.HookEvaluator
}

func New(evaluator workflows.HookEvaluator) *Service {
	return &Service{evaluator: evaluator}
}

func (s *Service) EvaluateComment(ctx context.Context, input handler.CommentWorkflowGateInput) (handler.CommentWorkflowGateResult, error) {
	allowed := handler.CommentWorkflowGateResult{Allowed: true, ParentID: input.ParentID}
	if input.AuthorType != "agent" || !input.WorkspaceID.Valid {
		return allowed, nil
	}
	if s == nil || s.evaluator == nil {
		return handler.CommentWorkflowGateResult{}, fmt.Errorf("before.message.send Workflow evaluator is unavailable")
	}

	eventID := fmt.Sprintf("comment:%x", sha256.Sum256([]byte(strings.Join([]string{
		input.TaskID,
		input.IssueID,
		input.AuthorID,
		input.ParentID,
		input.Content,
	}, "\x00"))))
	result, err := s.evaluator.Evaluate(ctx, workflows.HookEvent{
		EventID:     eventID,
		Type:        workflows.HookBeforeMessageSend,
		WorkspaceID: util.UUIDToString(input.WorkspaceID),
		IssueID:     input.IssueID,
		SessionID:   input.SessionID,
		AgentID:     input.AuthorID,
		Actor:       workflows.HookActor{Type: input.AuthorType, ID: input.AuthorID},
		MutableFields: []string{
			"parent_id",
		},
		Proposed: map[string]any{
			"content":   input.Content,
			"parent_id": input.ParentID,
		},
		Context: map[string]any{"message": map[string]any{
			"agent_authored":        true,
			"has_recipient":         hasRecipient(input.Content, input.AuthorID),
			"has_active_wakeup":     input.AgentHasActiveWakeup,
			"promises_continuation": promisesContinuation(input.Content),
			"thread_required":       input.ThreadRequired,
			"correct_thread":        !input.ThreadRequired || input.ParentID == input.RequiredParentID,
			"required_parent_id":    input.RequiredParentID,
			"no_action":             input.NoAction,
			"is_sub_issue":          input.IsSubIssue,
			"mentions_initiator":    mentionsOwner(input.Content, input.OwnerUserIDs),
			"mentions_agent":        mentionsAgent(input.Content, input.AuthorID),
			"posted_on_parent":      input.TaskPostedOnParent,
		}},
	})
	if err != nil {
		return allowed, err
	}
	if parentID, ok := result.Modifications["parent_id"].(string); ok {
		allowed.ParentID = parentID
	}
	if result.Decision == workflows.HookBlock || result.Decision == workflows.HookRequire {
		allowed.Allowed = false
		allowed.StatusCode = http.StatusUnprocessableEntity
		if input.NoAction {
			allowed.StatusCode = http.StatusConflict
		}
		allowed.Message = strings.Join(result.Requirements, " ")
		if allowed.Message == "" {
			allowed.Message = "comment blocked by Workflow policy"
		}
	}
	return allowed, nil
}

var continuationPromises = []string{
	"jeg fortsætter",
	"jeg kører videre",
	"jeg går i gang",
	"jeg vender tilbage",
	"jeg melder tilbage",
	"jeg følger op",
	"derefter lægger jeg",
	"jeg lægger den live",
	"fortsætter automatisk",
	"fortsættelsen er planlagt",
	"arbejdet fortsætter",
	"starter automatisk",
	"næste kørsel",
	"i will continue",
	"i'll continue",
	"i will follow up",
	"i'll follow up",
	"i will report back",
	"work continues automatically",
	"continues automatically",
	"next run will",
}

func promisesContinuation(content string) bool {
	lowered := strings.ToLower(content)
	for _, phrase := range continuationPromises {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return false
}

func hasRecipient(content, authorAgentID string) bool {
	for _, mention := range util.ParseMentions(content) {
		if mention.Type != "issue" && !isSelfAgentMention(mention, authorAgentID) {
			return true
		}
	}
	return false
}

func isSelfAgentMention(mention util.Mention, authorAgentID string) bool {
	return mention.Type == "agent" && authorAgentID != "" && strings.EqualFold(mention.ID, authorAgentID)
}

func mentionsAgent(content, authorAgentID string) bool {
	for _, mention := range util.ParseMentions(content) {
		if mention.Type == "agent" && !isSelfAgentMention(mention, authorAgentID) {
			return true
		}
	}
	return false
}

func mentionsOwner(content string, ownerUserIDs []string) bool {
	for _, mention := range util.ParseMentions(content) {
		if mention.Type != "member" {
			continue
		}
		for _, id := range ownerUserIDs {
			if mention.ID == id {
				return true
			}
		}
	}
	return false
}
