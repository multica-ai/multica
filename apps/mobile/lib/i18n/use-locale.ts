import { useT } from "./config";

// Use the UI locale for dates and numbers. The phone locale can differ from
// the selected app language, which would otherwise render mixed-language UI.
export function useLocale(): string {
  const { i18n } = useT();
  return i18n.resolvedLanguage ?? i18n.language;
}
