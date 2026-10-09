import { i18n } from "@/lib/i18n/singleton";

/**
 * Mobile time-ago formatter. Uses the app UI locale so relative timestamps
 * never disagree with surrounding strings, even when the device locale is
 * different from the user's explicit Multica language choice.
 */
export function timeAgo(dateStr: string): string {
  const t = i18n.t.bind(i18n);
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);

  if (minutes < 1) return t("common:time.just_now");
  if (minutes < 60) {
    return t("common:time.minutes_ago_other", { count: minutes });
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("common:time.hours_ago_other", { count: hours });
  const days = Math.floor(hours / 24);
  if (days < 7) return t("common:time.days_ago_other", { count: days });
  const weeks = Math.floor(days / 7);
  if (weeks < 5) return t("common:time.weeks_ago_other", { count: weeks });

  const locale = i18n.resolvedLanguage ?? i18n.language;
  return new Date(dateStr).toLocaleDateString(locale, {
    month: "short",
    day: "numeric",
  });
}
