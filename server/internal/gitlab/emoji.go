package gitlab

// Emoji translation between Multica's unicode convention and GitLab's named
// shortcode convention.
//
// Multica's UI uses emoji-mart and stores unicode (e.g. "👍") in the
// issue_reaction / comment_reaction cache. GitLab's award_emoji API expects
// named shortcodes (e.g. "thumbsup"). Translate at the wire boundary:
//
//   - Outbound (Multica → GitLab): EmojiUnicodeToShortcode
//   - Inbound (GitLab webhook → Multica): EmojiShortcodeToUnicode
//
// The cache always stores unicode. If a user picks an emoji not in this map,
// the handler silently falls through to Multica-only — the reaction is
// recorded locally but not round-tripped to GitLab. This mirrors the
// agent-reaction-Multica-only policy from Phase 3d.

// emojiUnicodeToShortcode covers the default emoji set the frontend's
// QuickEmojiPicker surfaces + the most common standard reactions. Extend as
// the picker's default set grows; unsupported emojis fall back to cache-only.
var emojiUnicodeToShortcode = map[string]string{
	// Quick-pick reactions
	"👍":  "thumbsup",
	"👎":  "thumbsdown",
	"❤️": "heart",
	"🎉":  "tada",
	"🚀":  "rocket",
	"👀":  "eyes",

	// Feelings
	"😀":  "grinning",
	"😃":  "smiley",
	"😄":  "smile",
	"😁":  "grin",
	"😂":  "joy",
	"😊":  "blush",
	"😉":  "wink",
	"😍":  "heart_eyes",
	"😎":  "sunglasses",
	"🤔":  "thinking",
	"😕":  "confused",
	"😢":  "cry",
	"😭":  "sob",
	"😡":  "rage",
	"😱":  "scream",
	"🤯":  "exploding_head",
	"🥳":  "partying_face",
	"🤩":  "star_struck",
	"🙃":  "upside_down_face",

	// Hands & symbols
	"👏":  "clap",
	"🙌":  "raised_hands",
	"🙏":  "pray",
	"💪":  "muscle",
	"👋":  "wave",
	"👌":  "ok_hand",
	"✌️": "v",

	// Status / feedback
	"🔥": "fire",
	"⭐": "star",
	"✨": "sparkles",
	"💯": "100",
	"✅": "white_check_mark",
	"❌": "x",
	"⚠️": "warning",
	"🚨": "rotating_light",
	"💡": "bulb",
	"🐛": "bug",
	"🎯": "dart",
	"📌": "pushpin",
	"🔗": "link",
}

// emojiShortcodeToUnicode is built at init from emojiUnicodeToShortcode.
// It's the reverse lookup used on webhook inbound.
var emojiShortcodeToUnicode = func() map[string]string {
	out := make(map[string]string, len(emojiUnicodeToShortcode))
	for u, s := range emojiUnicodeToShortcode {
		out[s] = u
	}
	return out
}()

// EmojiUnicodeToShortcode maps a unicode emoji to GitLab's named shortcode.
// Returns (shortcode, true) on a hit, ("", false) if the emoji isn't in the
// supported set. Callers should treat unsupported emojis as Multica-only
// (skip the GitLab round-trip; still persist locally).
func EmojiUnicodeToShortcode(unicode string) (string, bool) {
	s, ok := emojiUnicodeToShortcode[unicode]
	return s, ok
}

// EmojiShortcodeToUnicode maps a GitLab shortcode back to unicode.
// Returns (unicode, true) on a hit, ("", false) if the shortcode isn't in
// our supported set. Webhook handlers fall back to the raw shortcode when
// unmapped — the reaction still persists, just rendered as text. Extending
// the supported set is the long-term fix for "my reaction shows as 'facepalm'
// instead of an icon" user reports.
func EmojiShortcodeToUnicode(shortcode string) (string, bool) {
	u, ok := emojiShortcodeToUnicode[shortcode]
	return u, ok
}
