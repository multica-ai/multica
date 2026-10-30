import { useT } from "./use-t";

// The locale to hand to `Intl` / `toLocaleDateString` / `toLocaleString`.
//
// This is the language the member picked for the UI, which is NOT the same
// as `navigator.language`: someone reading a Chinese UI in an English-language
// browser must still get Chinese month and weekday names. Formatting against
// the browser language leaves half a screen in one language and half in
// another, which is exactly the bug this hook exists to prevent.
//
// `resolvedLanguage` is the language i18next actually rendered the strings
// with after fallback, so dates always agree with the text around them.
export function useLocale(): string {
  const { i18n } = useT();
  return i18n.resolvedLanguage ?? i18n.language;
}
