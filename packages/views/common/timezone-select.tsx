"use client";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

// Common IANA zones surfaced as quick picks. Used as the fallback option set
// when Intl.supportedValuesOf is not available, and promoted to the top of
// the list when it is.
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

export function timezoneOptions(current: string): string[] {
  const browser = browserTimezone();
  return Array.from(
    new Set([current, browser, ...COMMON_TIMEZONES, ...supportedTimezones()]),
  ).filter(Boolean);
}

// Shared single-select timezone picker. Surfaces the browser-resolved zone
// with a translated suffix (passed in by the caller — the picker itself stays
// i18n-namespace agnostic), followed by a curated set of common IANA zones
// and everything Intl.supportedValuesOf exposes.
export function TimezoneSelect({
  value,
  onValueChange,
  browserSuffix,
  disabled,
  triggerClassName,
}: {
  value: string;
  onValueChange: (next: string) => void;
  browserSuffix: string;
  disabled?: boolean;
  triggerClassName?: string;
}) {
  const browser = browserTimezone();
  const options = timezoneOptions(value);
  const render = (tz: string) =>
    tz === browser ? `${tz}${browserSuffix}` : tz;

  return (
    <Select
      value={value}
      disabled={disabled}
      onValueChange={(next) => {
        if (next) onValueChange(next);
      }}
    >
      <SelectTrigger
        size="sm"
        className={triggerClassName ?? "w-full rounded-md font-mono text-xs"}
      >
        <SelectValue>{render(value)}</SelectValue>
      </SelectTrigger>
      <SelectContent align="start" className="max-h-72">
        {options.map((tz) => (
          <SelectItem key={tz} value={tz} className="font-mono text-xs">
            {render(tz)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
