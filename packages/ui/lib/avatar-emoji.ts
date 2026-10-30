const AVATAR_EMOJI_PREFIX = "emoji:";

/**
 * Emojis offered as one-click choices by the avatar picker. Deliberately the
 * same set the server hands a brand-new agent at random
 * (`agentEmojiAvatars` in server/internal/handler/agent_avatar.go), so the
 * suggestions read as the product's own family of faces rather than an
 * unrelated second list. Eight per row.
 */
export const AVATAR_EMOJI_SUGGESTIONS = [
  "🐙", "🦊", "🦉", "🐝", "🐼", "🐸", "🐯", "🦁",
  "🐨", "🐵", "🐧", "🐳", "🦋", "🌞", "🌙", "⭐",
  "🔥", "⚡", "🍀", "🌈", "🚀", "🤖", "👾", "🧠",
];

export function parseAvatarEmoji(value?: string | null): string | null {
  if (!value?.startsWith(AVATAR_EMOJI_PREFIX)) return null;

  const emoji = value.slice(AVATAR_EMOJI_PREFIX.length).trim();
  return emoji || null;
}

/**
 * Builds the value to persist in `avatar_url` for an emoji avatar. The prefix
 * stays private to this module so the marker format has one owner on the
 * client, matching `agentEmojiAvatarPrefix` on the server.
 */
export function formatAvatarEmoji(emoji: string): string {
  return AVATAR_EMOJI_PREFIX + emoji.trim();
}
