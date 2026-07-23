package handler

import (
	"crypto/rand"
	"math/big"
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

func newAgentAvatar(avatarURL *string) pgtype.Text {
	if avatarURL != nil && strings.TrimSpace(*avatarURL) != "" {
		return pgtype.Text{String: *avatarURL, Valid: true}
	}
	return pgtype.Text{String: randomAgentEmojiAvatar(), Valid: true}
}
