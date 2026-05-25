/**
 * Mobile time-ago formatter. Mirrors the algorithm in
 * packages/views/inbox/components/inbox-list-item.tsx `useTimeAgo` so
 * "X minutes ago" reads identically across web/desktop and mobile (Behavioral
 * parity rule in apps/mobile/CLAUDE.md). The web version is i18n-driven via
 * useT; mobile v1 is English-only — when mobile ships i18n, mirror that
 * structure.
 */
export function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "Just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d ago`;
  const weeks = Math.floor(days / 7);
  if (weeks < 5) return `${weeks}w ago`;
  return new Date(dateStr).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}
