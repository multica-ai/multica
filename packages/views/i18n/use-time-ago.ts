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
// `soonForFuture`: a sub-minute gap buckets to the direction-less "just now"
// (it absorbs clock skew either way). For a genuinely scheduled future instant
// — the autopilot next run — that past-tense label reads wrong, so callers can
// opt into rendering imminent future times as "soon" instead.
export function useTimeAgo(opts?: { soonForFuture?: boolean }) {
  const { t } = useT("common");
  const timeZone = useViewingTimezone();
  return (dateStr: string): string => {
    const then = new Date(dateStr).getTime();
    const now = Date.now();
    const bucket = relativeTimeBucket(then, now, timeZone);
    switch (bucket.kind) {
      case "just_now":
        return opts?.soonForFuture && then > now
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
