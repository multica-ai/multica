import { useMemo } from "react";
import { useViewingTimezone } from "./use-viewing-timezone";
import { useT } from "../i18n";

export const COMPACT_INSTANT_FORMAT: Intl.DateTimeFormatOptions = {
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
};

export const FULL_INSTANT_FORMAT: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  timeZoneName: "short",
};

export const DATE_ONLY_FORMAT: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "short",
  day: "numeric",
};

export const TIME_WITH_SECONDS_FORMAT: Intl.DateTimeFormatOptions = {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
};

type DateInput = string | number | Date;

export interface FormatInstantOptions {
  locale: string;
  timeZone: string;
  options?: Intl.DateTimeFormatOptions;
}

function toDate(value: DateInput): Date {
  return value instanceof Date ? value : new Date(value);
}

function validTimeZone(timeZone: string): string {
  try {
    new Intl.DateTimeFormat("en-US", { timeZone }).format(new Date(0));
    return timeZone;
  } catch {
    return "UTC";
  }
}

export function formatInstant(
  value: DateInput,
  {
    locale,
    timeZone,
    options = COMPACT_INSTANT_FORMAT,
  }: FormatInstantOptions,
): string {
  const date = toDate(value);
  if (Number.isNaN(date.getTime())) return "";

  return new Intl.DateTimeFormat(locale || undefined, {
    ...options,
    timeZone: validTimeZone(timeZone),
  }).format(date);
}

export function useDateFormatter() {
  const { i18n } = useT();
  const timeZone = useViewingTimezone();

  return useMemo(
    () =>
      (value: DateInput, options?: Intl.DateTimeFormatOptions) =>
        formatInstant(value, {
          locale: i18n.language,
          timeZone,
          options,
        }),
    [i18n.language, timeZone],
  );
}
