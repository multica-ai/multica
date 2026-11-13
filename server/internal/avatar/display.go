// Package avatar holds the display-projection rules for avatar identity
// values (MAKE-291).
//
// The stored `avatar_url` column keeps its historical content: an uploaded
// image reference, an `emoji:<x>` legacy marker, or NULL. On top of that,
// agent rows now carry a persisted `avatar_seed` (migration 565) that is the
// agent's stable generated identity. Responses project the *display* value
// with this precedence (product decision, MAKE-291 / MAKE-301 finding H.1):
//
//  1. explicit uploaded/custom image
//  2. existing emoji marker `emoji:<x>` (a deliberate choice, never hidden)
//  3. generated marker `gen:<seed>` — only for agents that have neither
//  4. empty -> the client falls back to its Bot/initials placeholder
//
// The projection is deliberately display-only: no caller ever writes its
// output back to the database (acceptAvatarURL ignores `gen:` markers), so
// stored images and emoji stay persisted exactly as chosen and the seed
// cannot be destroyed by an API round-trip.
package avatar

import "strings"

// EmojiPrefix marks a legacy emoji avatar stored in avatar_url. It mirrors
// `agentEmojiAvatarPrefix` in the handler and `AVATAR_EMOJI_PREFIX` in
// packages/ui/lib/avatar-emoji.ts.
const EmojiPrefix = "emoji:"

// GeneratedPrefix marks a generated avatar whose payload is the persisted
// avatar_seed. Display-only — never stored.
const GeneratedPrefix = "gen:"

// IsEmoji reports whether a stored avatar value is a legacy emoji marker.
func IsEmoji(stored string) bool {
	return strings.HasPrefix(strings.TrimSpace(stored), EmojiPrefix)
}

// IsGenerated reports whether a value is a generated-avatar display marker.
func IsGenerated(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), GeneratedPrefix)
}

// Display projects the stored avatar value of a seeded entity (an agent)
// into the value responses should carry. `stored` is the raw avatar_url
// content; `seed` is the persisted avatar_seed ("" when absent).
//
// A seed exists for every agent (migration 565), so it must not override an
// explicit choice: an uploaded image or a chosen emoji keeps displaying, and
// the generated avatar is the default only when stored is empty. The result
// depends solely on (stored, seed) — never on name, provider, or runtime —
// so renaming or reconnecting cannot change it.
func Display(stored string, seed string) string {
	value := strings.TrimSpace(stored)
	if value != "" {
		// Image / data: URI / third-party profile URL / emoji marker — any
		// explicit choice wins over the generated identity (image > emoji).
		// A bare `emoji:` with no glyph is not a renderable choice, so it
		// does not hide the generated identity.
		if !strings.HasPrefix(value, EmojiPrefix) || strings.TrimSpace(strings.TrimPrefix(value, EmojiPrefix)) != "" {
			return value
		}
	}
	if trimmedSeed := strings.TrimSpace(seed); trimmedSeed != "" {
		return GeneratedPrefix + trimmedSeed
	}
	return ""
}
