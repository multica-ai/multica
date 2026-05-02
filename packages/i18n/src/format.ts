import { format as formatDateFn, formatDistanceToNow } from "date-fns";
import { enUS } from "date-fns/locale/en-US";
import { zhCN } from "date-fns/locale/zh-CN";
import type { Locale } from "./locales";

// Formatting facade. Views must NOT import date-fns directly — go through
// here so locale and format strings stay consistent across the app.

const DATE_FNS_LOCALES = {
  "zh-CN": zhCN,
  en: enUS,
} as const;

export type DatePreset = "short" | "long" | "datetime" | "relative";

const FORMAT_STRINGS: Record<Exclude<DatePreset, "relative">, Record<Locale, string>> = {
  short: { "zh-CN": "yyyy年M月d日", en: "MMM d, yyyy" },
  long: { "zh-CN": "yyyy年M月d日 EEEE", en: "EEEE, MMMM d, yyyy" },
  datetime: { "zh-CN": "yyyy年M月d日 HH:mm", en: "MMM d, yyyy h:mm a" },
};

export function formatDate(date: Date | number, preset: DatePreset, locale: Locale): string {
  if (preset === "relative") {
    return formatDistanceToNow(date, { addSuffix: true, locale: DATE_FNS_LOCALES[locale] });
  }
  return formatDateFn(date, FORMAT_STRINGS[preset][locale], { locale: DATE_FNS_LOCALES[locale] });
}

export function formatNumber(value: number, locale: Locale, options?: Intl.NumberFormatOptions): string {
  return new Intl.NumberFormat(locale, options).format(value);
}

export function formatCurrency(value: number, currency: string, locale: Locale): string {
  return new Intl.NumberFormat(locale, { style: "currency", currency }).format(value);
}
