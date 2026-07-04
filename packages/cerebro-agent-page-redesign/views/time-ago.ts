/**
 * Compact relative-time label ("3m", "6h", "62d") for the identity rail's
 * Created / Updated rows. Kept local and dependency-free — the redesign only
 * needs a coarse label, not a full i18n date library.
 */
export function timeAgo(iso: string | null | undefined): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  // Keep day-granularity for a while (the identity rail shows e.g. "62d ago"
  // before collapsing to months) so recent-but-not-fresh agents read naturally.
  if (days < 90) return `${days}d ago`;
  const months = Math.round(days / 30);
  if (months < 12) return `${months}mo ago`;
  return `${Math.round(months / 12)}y ago`;
}
