package service

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	chatQuickActionsFence    = "```quick-actions\n"
	chatQuickActionMaxCount  = 3
	chatQuickActionLabelMax  = 80
	chatQuickActionPromptMax = 500
)

// splitChatQuickActions separates one reserved trailing quick-actions fence
// from the visible reply. A recognized footer is always stripped, including
// when its JSON is malformed, so private control syntax never leaks into Chat.
// A mid-response fence is ordinary user-visible markdown and is left intact.
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

	visible = strings.TrimRight(visible, " \t\n")
	var candidates []protocol.ChatQuickAction
	if err := json.Unmarshal([]byte(raw), &candidates); err != nil {
		return visible, nil
	}

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
	return visible, actions
}

func truncateChatQuickAction(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes-1]) + "…"
}
