"use client";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

// Common IANA zones offered as quick picks when Intl.supportedValuesOf is not
// available, and promoted near the top otherwise.
const COMMON_TIMEZONES = [
  "UTC",
  "America/Los_Angeles",
  "America/Denver",
  "America/Chicago",
  "America/New_York",
  "America/Sao_Paulo",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Paris",
  "Europe/Moscow",
  "Africa/Cairo",
  "Asia/Dubai",
  "Asia/Kolkata",
  "Asia/Bangkok",
  "Asia/Shanghai",
  "Asia/Singapore",
  "Asia/Tokyo",
  "Australia/Sydney",
  "Pacific/Auckland",
];

export function browserTimezone(): string {
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    return tz || "UTC";
  } catch {
    return "UTC";
  }
}

type IntlWithSupportedValues = typeof Intl & {
  supportedValuesOf?: (key: "timeZone") => string[];
};

function supportedTimezones(): string[] {
  try {
    const supported = (Intl as IntlWithSupportedValues).supportedValuesOf?.(
      "timeZone",
    );
    return supported && supported.length > 0 ? supported : COMMON_TIMEZONES;
  } catch {
    return COMMON_TIMEZONES;
  }
}

export function buildTimezoneOptions(current: string): string[] {
  return Array.from(
    new Set([current, browserTimezone(), ...COMMON_TIMEZONES, ...supportedTimezones()]),
  ).filter(Boolean);
}

export interface TimezoneSelectProps {
  value: string;
  onValueChange: (next: string) => void;
  disabled?: boolean;
  /** Custom classes applied to the trigger element. */
  triggerClassName?: string;
  /** Size prop forwarded to <SelectTrigger>. */
  size?: "sm" | "default";
}

/**
 * Controlled timezone dropdown shared between the runtime detail page and the
 * usage page. Lists the supplied value first, then the browser's resolved
 * zone, then a curated set of common IANA zones, and finally everything
 * `Intl.supportedValuesOf("timeZone")` exposes — de-duplicated. The browser
 * zone is suffixed via the `runtimes.detail.timezone_browser_suffix` string
 * so users can see at a glance which entry matches their local clock.
 */
export function TimezoneSelect({
  value,
  onValueChange,
  disabled,
  triggerClassName,
  size = "sm",
}: TimezoneSelectProps) {
  const { t } = useT("runtimes");
  const current = value || "UTC";
  const browser = browserTimezone();
  const browserSuffix = t(($) => $.detail.timezone_browser_suffix);
  const options = buildTimezoneOptions(current);

  return (
    <Select
      value={current}
      disabled={disabled}
      onValueChange={(next) => {
        if (next && next !== current) onValueChange(next);
      }}
    >
      <SelectTrigger
        size={size}
        className={cn("rounded-md font-mono text-xs", triggerClassName)}
      >
        <SelectValue>
          {current === browser ? `${current}${browserSuffix}` : current}
        </SelectValue>
      </SelectTrigger>
      <SelectContent align="start" className="max-h-72">
        {options.map((tz) => (
          <SelectItem key={tz} value={tz} className="font-mono text-xs">
            {tz === browser ? `${tz}${browserSuffix}` : tz}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
