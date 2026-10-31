import { useT } from "./use-t";

function formatZhDateTime(dateStr: string): string {
  const date = new Date(dateStr);
  const year = date.getFullYear();
  const month = date.getMonth() + 1;
  const day = date.getDate();
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  return `${year}年${month}月${day}日 ${hours}:${minutes}`;
}

// Localized relative-time formatter. Returns a function so call-site usage
// stays terse: `const timeAgo = useTimeAgo(); ...timeAgo(dateStr)`.
export function useTimeAgo() {
  const { t, i18n } = useT("common");
  return (dateStr: string): string => {
    if (i18n.language === "zh-Hans") {
      return formatZhDateTime(dateStr);
    }

    const diff = Date.now() - new Date(dateStr).getTime();
    const minutes = Math.floor(diff / 60000);
    if (minutes < 1) return t(($) => $.time.just_now);
    if (minutes < 60) return t(($) => $.time.minutes_ago, { count: minutes });
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return t(($) => $.time.hours_ago, { count: hours });
    const days = Math.floor(hours / 24);
    return t(($) => $.time.days_ago, { count: days });
  };
}
