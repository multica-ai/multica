import { useCallback } from "react";
import { useT } from "../i18n";

// Runtime "last seen" formatter. Distinct from the shared useTimeAgo gradient:
// it keeps SECONDS-level precision and compound units ("2m 30s ago",
// "2d 4h ago") because runtime liveness is about connection freshness, where a
// 30s-vs-5m difference matters. Localized via the runtimes namespace; the
// visible value pairs with an <InstantTooltip> at the call site for the exact
// instant.
export function useFormatLastSeen(): (lastSeenAt: string | null) => string {
  const { t } = useT("runtimes");
  return useCallback((lastSeenAt) => {
    if (!lastSeenAt) return t(($) => $.last_seen.never);
    const diffMs = Date.now() - new Date(lastSeenAt).getTime();
    if (diffMs < 5_000) return t(($) => $.last_seen.just_now);

    const seconds = Math.floor(diffMs / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    if (minutes < 1) return t(($) => $.last_seen.seconds_ago, { count: seconds });
    if (hours < 1) {
      const s = seconds % 60;
      return s > 0
        ? t(($) => $.last_seen.minutes_seconds_ago, { minutes, seconds: s })
        : t(($) => $.last_seen.minutes_ago, { count: minutes });
    }
    if (days < 1) {
      const m = minutes % 60;
      return m > 0
        ? t(($) => $.last_seen.hours_minutes_ago, { hours, minutes: m })
        : t(($) => $.last_seen.hours_ago, { count: hours });
    }
    const h = hours % 24;
    return h > 0
      ? t(($) => $.last_seen.days_hours_ago, { days, hours: h })
      : t(($) => $.last_seen.days_ago, { count: days });
  }, [t]);
}
