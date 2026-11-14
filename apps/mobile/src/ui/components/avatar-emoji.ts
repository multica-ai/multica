// The `avatar_url` marker for an emoji avatar. Must match packages/ui/lib/avatar-emoji.ts
// and agentEmojiAvatarPrefix on the server, which gives every new agent one.
const AVATAR_EMOJI_PREFIX = "emoji:";

export function parseAvatarEmoji(value?: string | null): string | null {
  if (!value?.startsWith(AVATAR_EMOJI_PREFIX)) return null;
  const emoji = value.slice(AVATAR_EMOJI_PREFIX.length).trim();
  return emoji || null;
}
