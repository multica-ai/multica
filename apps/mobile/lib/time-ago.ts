import { relativeTimeBucket } from "@multica/core/i18n/relative-time";

/**
 * Mobile time-ago formatter. The gradient and cutoffs are shared with
 * web/desktop via `relativeTimeBucket` in @multica/core (Behavioral parity
 * rule in apps/mobile/CLAUDE.md), so the labels read identically across
 * platforms. It is symmetric: past instants read "3h ago", future ones "in
 * 3h". Day granularity is calendar-based; the device timezone stands in for
 * the viewer's Viewing Timezone (web reads `user.timezone`). Past the day cap
 * the gradient continues into calendar months, then years — no absolute-date
 * fallback. The web version is i18n-driven via useT; mobile v1 is English-only
 * — when mobile ships i18n, map the same buckets to translations.
 */
export function timeAgo(dateStr: string): string {
  const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const bucket = relativeTimeBucket(
    new Date(dateStr).getTime(),
    Date.now(),
    timeZone,
  );
  switch (bucket.kind) {
    case "just_now":
      return "Just now";
    case "minutes":
      return bucket.future ? `in ${bucket.count}m` : `${bucket.count}m ago`;
    case "hours":
      return bucket.future ? `in ${bucket.count}h` : `${bucket.count}h ago`;
    case "days":
      return bucket.future ? `in ${bucket.count}d` : `${bucket.count}d ago`;
    case "months":
      return bucket.future ? `in ${bucket.count}mo` : `${bucket.count}mo ago`;
    case "years":
      return bucket.future ? `in ${bucket.count}y` : `${bucket.count}y ago`;
    default:
      return "—";
  }
}
