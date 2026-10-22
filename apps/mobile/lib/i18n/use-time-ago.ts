import { useT } from "./config";

export function useTimeAgo() {
  const { t } = useT("common");

  return (dateStr: string): string => {
    const diff = Date.now() - new Date(dateStr).getTime();
    const minutes = Math.floor(diff / 60000);

    if (minutes < 1) return t("time.just_now");
    if (minutes < 60) return t("time.minutes_ago_other", { count: minutes });
    if (minutes < 60 * 24) {
      return t("time.hours_ago_other", { count: Math.floor(minutes / 60) });
    }
    if (minutes < 60 * 24 * 7) {
      return t("time.days_ago_other", { count: Math.floor(minutes / (60 * 24)) });
    }
    if (minutes < 60 * 24 * 35) {
      return t("time.weeks_ago_other", { count: Math.floor(minutes / (60 * 24 * 7)) });
    }

    return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(
      new Date(dateStr),
    );
  };
}
