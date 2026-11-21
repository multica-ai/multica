export type DialogLocale =
  | "en"
  | "zh-Hans"
  | "zh-Hant"
  | "ja"
  | "ko"
  | "fr"
  | "es"
  | "ru";

export function resolveDialogLocale(preferredLanguage: string): DialogLocale {
  const preferred = preferredLanguage.toLowerCase();
  if (preferred.startsWith("zh")) {
    return /^zh-(hant|tw|hk|mo)\b/.test(preferred) ? "zh-Hant" : "zh-Hans";
  }
  if (preferred.startsWith("ja")) return "ja";
  if (preferred.startsWith("ko")) return "ko";
  if (preferred.startsWith("fr")) return "fr";
  if (preferred.startsWith("es")) return "es";
  if (preferred.startsWith("ru")) return "ru";
  return "en";
}
