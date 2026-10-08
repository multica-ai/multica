// Package avatar holds the display-projection rules for avatar identity
// values (MAKE-291).
//
// The stored `avatar_url` column keeps its historical content: an uploaded
// image reference, an `emoji:<x>` legacy marker, or NULL. On top of that,
// agent rows now carry a persisted `avatar_seed` (migration 565) that is the
// agent's stable generated identity. Responses project the *display* value
// with this precedence:
//
//  1. explicit uploaded/custom image (never suppressed by a seed)
//  2. generated marker `gen:<seed>` when a seed exists
//  3. legacy `emoji:<x>` marker (seedless rows only)
//  4. empty -> the client falls back to its Bot/initials placeholder
//
// The projection is deliberately display-only: no caller ever writes its
// output back to the database (acceptAvatarURL ignores `gen:` markers), so
// hand-picked emoji stay persisted as legacy/fallback identity and the seed
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
// An explicit image always wins: a seed exists for every agent but must not
// override an uploaded/custom avatar. An emoji marker only survives when no
// seed exists, so a generated identity is never suppressed by legacy
// emoji data. The result depends solely on (stored, seed) — never on name,
// provider, or runtime — so renaming or reconnecting cannot change it.
func Display(stored string, seed string) string {
	value := strings.TrimSpace(stored)
	if value != "" && !strings.HasPrefix(value, EmojiPrefix) {
		// Image / data: URI / third-party profile URL — explicit choice wins.
		return value
	}
	if trimmedSeed := strings.TrimSpace(seed); trimmedSeed != "" {
		return GeneratedPrefix + trimmedSeed
	}
	return value
}
