import { relativeTimeBucket } from "@multica/core/i18n/relative-time";
import { useViewingTimezone } from "../common/use-viewing-timezone";
import { useT } from "./use-t";

// Localized relative-time formatter. Returns a function so call-site usage
// stays terse: `const timeAgo = useTimeAgo(); ...timeAgo(dateStr)`.
//
// The gradient lives in `@multica/core/i18n/relative-time` (shared with
// mobile) and is symmetric: a past instant reads "3h ago", a future one
// "in 3h". Day granularity is calendar-based in the viewer's Viewing Timezone;
// past the day cap it continues into calendar months, then years — there is no
// absolute-date fallback.
// `scheduled`: the instant is a scheduled run — the autopilot next run. Render
// it from the scheduler's view: an already-due slot the scheduler hasn't
// advanced yet (next_run_at in the past) is imminent, not bygone, so it reads
// "soon" rather than a misleading "Xm ago"; likewise a sub-minute-away future
// run reads "soon" instead of the direction-less "just now" (which otherwise
// absorbs clock skew either way).
export function useTimeAgo(opts?: { scheduled?: boolean }) {
  const { t } = useT("common");
  const timeZone = useViewingTimezone();
  return (dateStr: string): string => {
    const then = new Date(dateStr).getTime();
    const now = Date.now();
    // Overdue scheduled slot the scheduler hasn't rolled forward yet → imminent.
    if (opts?.scheduled && then <= now) return t(($) => $.time.soon);
    const bucket = relativeTimeBucket(then, now, timeZone);
    switch (bucket.kind) {
      case "just_now":
        return opts?.scheduled && then > now
          ? t(($) => $.time.soon)
          : t(($) => $.time.just_now);
      case "minutes":
        return t(($) => (bucket.future ? $.time.in_minutes : $.time.minutes_ago), {
          count: bucket.count,
        });
      case "hours":
        return t(($) => (bucket.future ? $.time.in_hours : $.time.hours_ago), {
          count: bucket.count,
        });
      case "days":
        return t(($) => (bucket.future ? $.time.in_days : $.time.days_ago), {
          count: bucket.count,
        });
      case "months":
        return t(($) => (bucket.future ? $.time.in_months : $.time.months_ago), {
          count: bucket.count,
        });
      case "years":
        return t(($) => (bucket.future ? $.time.in_years : $.time.years_ago), {
          count: bucket.count,
        });
      default:
        return "—";
    }
  };
}
