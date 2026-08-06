package handler

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

const agentEmojiAvatarPrefix = "emoji:"

var agentEmojiAvatars = []string{
	"🐙", "🦊", "🦉", "🐝", "🐼", "🐸", "🐯", "🦁",
	"🐨", "🐵", "🐧", "🐳", "🦋", "🌞", "🌙", "⭐",
	"🔥", "⚡", "🍀", "🌈", "🚀", "🤖", "👾", "🧠",
}

func randomAgentEmojiAvatar() string {
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(agentEmojiAvatars))))
	if err != nil {
		return agentEmojiAvatarPrefix + agentEmojiAvatars[0]
	}
	return agentEmojiAvatarPrefix + agentEmojiAvatars[index.Int64()]
}

// newAgentAvatar resolves the avatar to persist for a newly created agent. An
// explicit value is validated through acceptAvatarURL — a create path is still
// a way to publish a storage object, so it carries the same authorization
// boundary as an update. ok=false means the error response is already written
// and the caller must abort.
func (h *Handler) newAgentAvatar(w http.ResponseWriter, r *http.Request, avatarURL *string) (pgtype.Text, bool) {
	if avatarURL != nil && strings.TrimSpace(*avatarURL) != "" {
		accepted, ok := h.acceptAvatarURL(w, r, *avatarURL, "")
		if !ok {
			return pgtype.Text{}, false
		}
		return pgtype.Text{String: accepted, Valid: true}, true
	}
	return pgtype.Text{String: randomAgentEmojiAvatar(), Valid: true}, true
}
