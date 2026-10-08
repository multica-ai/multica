package handler

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/avatar"
)

const agentEmojiAvatarPrefix = "emoji:"

// agentEmojiAvatars is the legacy emoji suggestion set. Kept as the canonical
// server-side copy the client suggestions mirror (packages/ui/lib/
// avatar-emoji.ts); a brand-new agent is no longer handed one at random —
// avatar_seed (column DEFAULT, migration 565) is its identity now.
var agentEmojiAvatars = []string{
	"🐙", "🦊", "🦉", "🐝", "🐼", "🐸", "🐯", "🦁",
	"🐨", "🐵", "🐧", "🐳", "🦋", "🌞", "🌙", "⭐",
	"🔥", "⚡", "🍀", "🌈", "🚀", "🤖", "👾", "🧠",
}

// newAgentAvatar resolves the avatar_url to persist for a newly created
// agent (MAKE-291). An explicit value is validated through acceptAvatarURL —
// a create path is still a way to publish a storage object, so it carries the
// same authorization boundary as an update. ok=false means the error response
// is already written and the caller must abort.
//
// When the caller omits the avatar, avatar_url stays NULL: a random emoji is
// no longer assigned at creation. The agent's visual identity comes from
// avatar_seed instead, which the column DEFAULT mints in the same INSERT
// (migration 565); avatar.Display projects (NULL, seed) into `gen:<seed>` on
// the way out.
func (h *Handler) newAgentAvatar(w http.ResponseWriter, r *http.Request, avatarURL *string) (pgtype.Text, bool) {
	if avatarURL != nil && strings.TrimSpace(*avatarURL) != "" {
		// A `gen:` display marker is not a storable avatar — treat it as if
		// the caller omitted the field so avatar_url stays NULL (MAKE-291).
		if avatar.IsGenerated(strings.TrimSpace(*avatarURL)) {
			return pgtype.Text{}, true
		}
		accepted, ok := h.acceptAvatarURL(w, r, *avatarURL, "")
		if !ok {
			return pgtype.Text{}, false
		}
		return pgtype.Text{String: accepted, Valid: true}, true
	}
	return pgtype.Text{}, true
}

// projectAvatarDisplay projects a stored (avatar_url, avatar_seed) pair into
// the display value an API response carries (MAKE-291): explicit image >
// `gen:<seed>` > legacy `emoji:<x>` > empty (client-side placeholder).
// Stored image URLs go through resolveAvatarURL so storage objects keep
// being signed exactly as before; marker values pass through untouched.
// The result is display-only — writes are guarded in acceptAvatarURL.
func (h *Handler) projectAvatarDisplay(stored pgtype.Text, seed string) *string {
	raw := ""
	if stored.Valid {
		raw = stored.String
	}
	display := avatar.Display(raw, seed)
	if display == "" {
		return nil
	}
	if avatar.IsGenerated(display) || avatar.IsEmoji(display) {
		resolved := display
		return &resolved
	}
	return h.resolveAvatarURLPtr(&display)
}
