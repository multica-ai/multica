package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	chatQuickActionsFence    = "```quick-actions\n"
	chatQuickActionMaxCount  = 3
	chatQuickActionLabelMax  = 80
	chatQuickActionPromptMax = 500
)

// parseChatQuickActionsOutput parses the daemon suggestion pass's raw output
// into sanitized actions. The pass is instructed to emit a bare JSON array,
// but this parser is deliberately lenient — models wrap output in code fences
// or lead with a sentence often enough that strict parsing would silently
// drop good suggestions. Anything unparseable degrades to "no suggestions";
// this output never reaches the transcript, so there is nothing to leak.
func parseChatQuickActionsOutput(raw string) []protocol.ChatQuickAction {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Attempt order narrows from strict to desperate: the bare array the pass
	// was asked for, then the inside of a code fence, then the outermost
	// bracket span. The bracket scan runs last because leading prose may
	// itself contain brackets ("here's [my] take: [...]"), which would
	// misalign the slice if it were tried first.
	for _, candidate := range []string{raw, insideCodeFence(raw)} {
		if actions, ok := unmarshalChatQuickActions(candidate); ok {
			return actions
		}
	}
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil
	}
	actions, _ := unmarshalChatQuickActions(raw[start : end+1])
	return actions
}

// insideCodeFence returns the content of the first fenced code block in raw
// (language tag tolerated), or "" when no complete fence exists.
func insideCodeFence(raw string) string {
	open := strings.Index(raw, "```")
	if open < 0 {
		return ""
	}
	rest := raw[open+3:]
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:] // drop the opening fence's language line
	} else {
		return ""
	}
	closing := strings.Index(rest, "```")
	if closing < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:closing])
}

func unmarshalChatQuickActions(raw string) ([]protocol.ChatQuickAction, bool) {
	if raw == "" {
		return nil, false
	}
	var candidates []protocol.ChatQuickAction
	if err := json.Unmarshal([]byte(raw), &candidates); err != nil {
		return nil, false
	}
	return sanitizeChatQuickActions(candidates), true
}

// splitChatQuickActions separates one reserved trailing quick-actions fence
// from the visible reply. A recognized footer is always stripped, including
// when its JSON is malformed, so private control syntax never leaks into Chat.
// A mid-response fence is ordinary user-visible markdown and is left intact.
//
// The in-band footer is no longer the primary suggestion source — the daemon
// suggestion pass is (parseChatQuickActionsOutput). This split stays for two
// reasons: older daemons still inject the retired brief instruction, and
// provider sessions created before the upgrade carry the syntax in their own
// history, so agents keep emitting footers for a while either way.
func splitChatQuickActions(output string) (string, []protocol.ChatQuickAction) {
	normalized := strings.ReplaceAll(output, "\r\n", "\n")
	trimmed := strings.TrimRight(normalized, " \t\n")
	if !strings.HasSuffix(trimmed, "\n```") {
		return output, nil
	}

	withoutClose := strings.TrimSuffix(trimmed, "\n```")
	marker := "\n" + chatQuickActionsFence
	start := strings.LastIndex(withoutClose, marker)
	visible := ""
	raw := ""
	switch {
	case start >= 0:
		visible = withoutClose[:start]
		raw = withoutClose[start+len(marker):]
	case strings.HasPrefix(withoutClose, chatQuickActionsFence):
		raw = strings.TrimPrefix(withoutClose, chatQuickActionsFence)
	default:
		return output, nil
	}
	// A quick-actions fence that closes before the end of the reply is ordinary
	// visible markdown, even when a later, unrelated code fence happens to end
	// the whole message. Without this guard the final closing fence above can be
	// paired with a mid-response opener and silently truncate everything after
	// that example.
	if strings.Contains(raw, "\n```") {
		return output, nil
	}

	visible = strings.TrimRight(visible, " \t\n")
	var candidates []protocol.ChatQuickAction
	if err := json.Unmarshal([]byte(raw), &candidates); err != nil {
		return visible, nil
	}
	return visible, sanitizeChatQuickActions(candidates)
}

// sanitizeChatQuickActions enforces the server-side contract on agent-supplied
// candidates regardless of which channel they arrived through: at most three
// actions, normalized non-empty labels, case-insensitive label dedup, prompts
// defaulting to the label, rune-safe truncation, and exactly one primary.
func sanitizeChatQuickActions(candidates []protocol.ChatQuickAction) []protocol.ChatQuickAction {
	actions := make([]protocol.ChatQuickAction, 0, min(len(candidates), chatQuickActionMaxCount))
	seen := make(map[string]struct{}, chatQuickActionMaxCount)
	primarySeen := false
	for _, candidate := range candidates {
		if len(actions) == chatQuickActionMaxCount {
			break
		}
		label := strings.Join(strings.Fields(candidate.Label), " ")
		if label == "" {
			continue
		}
		label = truncateChatQuickAction(label, chatQuickActionLabelMax)
		key := strings.ToLower(label)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}

		prompt := strings.TrimSpace(candidate.Prompt)
		if prompt == "" {
			prompt = label
		}
		prompt = truncateChatQuickAction(prompt, chatQuickActionPromptMax)
		primary := candidate.Primary && !primarySeen
		primarySeen = primarySeen || primary
		actions = append(actions, protocol.ChatQuickAction{
			Label:   label,
			Prompt:  prompt,
			Primary: primary,
		})
	}
	if len(actions) > 0 && !primarySeen {
		actions[0].Primary = true
	}
	return actions
}

func truncateChatQuickAction(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes-1]) + "…"
}

// chatQuickActionsPending decides whether the chat:done broadcast should tell
// clients to expect a chat:quick_actions supplement: the daemon declared one
// on the complete callback, an ordinary assistant message was written, and no
// actions are attached yet (an in-band fallback already delivered would make
// a placeholder pointless).
func chatQuickActionsPending(result []byte, msg *db.ChatMessage) bool {
	var payload protocol.TaskCompletedPayload
	_ = json.Unmarshal(result, &payload)
	if !payload.QuickActionsPending || msg == nil {
		return false
	}
	if msg.MessageKind != protocol.ChatMessageKindMessage {
		return false
	}
	var existing []protocol.ChatQuickAction
	_ = json.Unmarshal(msg.QuickActions, &existing)
	return len(existing) == 0
}

// SupplementChatQuickActions attaches the daemon suggestion pass's output to
// the completed turn's assistant message and broadcasts chat:quick_actions.
// Best-effort semantics: an unparseable or empty result still broadcasts the
// row's current (usually empty) actions so pending placeholders resolve. A
// turn that never wrote an assistant row (no_response, channel empty-drop)
// returns silently — no client is waiting in that case, because the pending
// flag is only ever raised for a written ordinary message.
func (s *TaskService) SupplementChatQuickActions(ctx context.Context, task db.AgentTaskQueue, raw string) error {
	if !task.ChatSessionID.Valid {
		return nil
	}
	actions := parseChatQuickActionsOutput(raw)
	for i := range actions {
		actions[i].Label = redact.Text(actions[i].Label)
		actions[i].Prompt = redact.Text(actions[i].Prompt)
	}

	var msg db.ChatMessage
	var err error
	if len(actions) > 0 {
		encoded, marshalErr := json.Marshal(actions)
		if marshalErr != nil {
			return fmt.Errorf("marshal chat quick actions: %w", marshalErr)
		}
		msg, err = s.Queries.SetChatMessageQuickActionsByTask(ctx, db.SetChatMessageQuickActionsByTaskParams{
			TaskID:       task.ID,
			QuickActions: encoded,
		})
	} else {
		msg, err = s.Queries.GetChatMessageByTaskAssistant(ctx, task.ID)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("attach chat quick actions: %w", err)
	}

	workspaceID := s.ResolveTaskWorkspaceID(ctx, task)
	if workspaceID == "" {
		return nil
	}
	payload := protocol.ChatQuickActionsPayload{
		ChatSessionID: util.UUIDToString(task.ChatSessionID),
		TaskID:        util.UUIDToString(task.ID),
		MessageID:     util.UUIDToString(msg.ID),
		QuickActions:  []protocol.ChatQuickAction{},
	}
	_ = json.Unmarshal(msg.QuickActions, &payload.QuickActions)
	s.Bus.Publish(events.Event{
		Type:          protocol.EventChatQuickActions,
		WorkspaceID:   workspaceID,
		ActorType:     "system",
		ActorID:       "",
		ChatSessionID: util.UUIDToString(task.ChatSessionID),
		Payload:       payload,
	})
	return nil
}
