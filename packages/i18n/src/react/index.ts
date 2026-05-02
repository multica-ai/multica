// Universal React bindings — works in any React env (Next.js, Vite, Electron).
// Re-exported from use-intl so packages/views can import without depending on next.
// Next.js Server Components in apps/web should import from "next-intl/server" directly.

export {
  IntlProvider,
  useTranslations,
  useFormatter,
  useLocale,
  useNow,
  useTimeZone,
  useMessages,
} from "use-intl";
export type { AbstractIntlMessages, Formats } from "use-intl";
