import { useCallback } from "react";
import { formatDateOnly } from "@multica/core/issues/date";
import { useT } from "../i18n/use-t";

/**
 * Locale-aware formatter for FLOATING calendar days (issue start_date /
 * due_date). It stays UTC-anchored (the day never shifts with timezone — that
 * is `formatDateOnly`'s job); this hook only injects the Language locale so
 * callers stop hardcoding "en-US".
 */
export function useFormatCalendarDate(): (
  value: string | null | undefined,
  options?: Intl.DateTimeFormatOptions,
) => string {
  const { i18n } = useT("common");
  const locale = i18n.language || "en";
  return useCallback(
    (value, options) => formatDateOnly(value, options, locale),
    [locale],
  );
}
