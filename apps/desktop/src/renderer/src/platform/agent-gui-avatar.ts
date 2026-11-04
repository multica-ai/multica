import { parseAvatarEmoji } from "@multica/ui/lib/avatar-emoji";

/** The embedded GUI accepts image URLs, while Multica also stores emoji avatars. */
export function agentGuiAvatarUrl(
  avatarUrl: string | null | undefined,
  resolveImage: (url: string) => string,
): string {
  const emoji = parseAvatarEmoji(avatarUrl);
  if (avatarUrl && !avatarUrl.startsWith("emoji:")) return resolveImage(avatarUrl);

  const text = (emoji ?? "🤖").replace(/[&<>"']/g, (character) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&apos;",
  })[character]!);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><text x="16" y="16" dy=".35em" text-anchor="middle" font-size="20" font-family="Apple Color Emoji,Segoe UI Emoji,Noto Color Emoji,sans-serif">${text}</text></svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}#multica-agent-avatar`;
}
