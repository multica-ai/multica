package handler

import (
	"encoding/json"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Only SendDirectChatMessage stamps the authenticated sender. Attribution is
// deliberately insufficient: delegated agent runs inherit a human originator.
func memberChatSkillMessage(context []byte, messages []db.ChatMessage) *string {
	var source struct {
		ActorType string `json:"chat_input_actor_type"`
	}
	if json.Unmarshal(context, &source) != nil || source.ActorType == "" {
		// A queued task from before sender stamping retains the legacy hint,
		// just like a claim from an older server. It cannot invoke a skill.
		return nil
	}
	message := ""
	if source.ActorType != "member" {
		return &message
	}
	var parts []string
	for _, message := range messages {
		if message.Role == "user" && message.MessageKind == protocol.ChatMessageKindMessage && !message.ChannelIngested && strings.TrimSpace(message.Content) != "" {
			parts = append(parts, message.Content)
		}
	}
	message = strings.Join(parts, "\n\n")
	return &message
}
