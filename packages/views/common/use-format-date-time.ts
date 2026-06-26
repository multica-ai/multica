import { useMemo } from "react";
import {
  formatInstant,
  formatInstantWithOffset,
} from "@multica/core/i18n/format-date-time";
import { useT } from "../i18n/use-t";
import { useViewingTimezone } from "./use-viewing-timezone";

export type InstantValue = string | number | Date | null | undefined;

export interface InstantFormatters {
  /** Full date + time in the viewer's timezone + locale. */
  formatDateTime: (value: InstantValue) => string;
  /** Date only in the viewer's timezone + locale. */
  formatDate: (value: InstantValue) => string;
  /** Time only in the viewer's timezone + locale. */
  formatTime: (value: InstantValue) => string;
  /** Tooltip string: full date-time + GMT offset suffix. */
  formatTooltip: (value: InstantValue) => string;
}

/**
 * Formatters for INSTANTS (TIMESTAMPTZ values), bound to the viewer's Viewing
 * Timezone (`useViewingTimezone`) and Language locale (`i18n.language`). The
 * i18next short codes ("en" / "zh-Hans" / ...) are valid BCP-47 and pass
 * straight to Intl. This is the single replacement for inline
 * `new Date(x).toLocaleString(...)`, which would use the browser timezone.
 */
export function useFormatDateTime(): InstantFormatters {
  const timeZone = useViewingTimezone();
  const { i18n } = useT("common");
  const locale = i18n.language || "en";

  return useMemo(() => {
    const opts = { locale, timeZone };
    return {
      formatDateTime: (value) =>
        formatInstant(value, { ...opts, mode: "datetime" }),
      formatDate: (value) => formatInstant(value, { ...opts, mode: "date" }),
      formatTime: (value) => formatInstant(value, { ...opts, mode: "time" }),
      formatTooltip: (value) => formatInstantWithOffset(value, opts),
    };
  }, [locale, timeZone]);
}

/**
 * The instant tooltip formatter alone — full date-time + GMT offset in the
 * viewer's Viewing Timezone + Language locale. For callers that render their own
 * visible text (relative time, an interpolated phrase) and only need the hover
 * tooltip, so they don't build the unused date/time/datetime formatters the full
 * `useFormatDateTime` would.
 */
export function useFormatInstantTooltip(): (value: InstantValue) => string {
  const timeZone = useViewingTimezone();
  const { i18n } = useT("common");
  const locale = i18n.language || "en";

  return useMemo(
    () => (value: InstantValue) =>
      formatInstantWithOffset(value, { locale, timeZone }),
    [locale, timeZone],
  );
}
