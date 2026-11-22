export type DialogLocale =
  | "en"
  | "zh-Hans"
  | "zh-Hant"
  | "ja"
  | "ko"
  | "fr"
  | "es"
  | "ru";

let appLocaleOverride: string | null = null;

/**
 * Record the renderer's resolved in-app locale so main-process native dialogs
 * and daemon error strings match the user's Settings choice instead of only
 * following the OS language.
 */
export function setAppDialogLocale(locale: string | null): void {
  const trimmed = typeof locale === "string" ? locale.trim() : "";
  appLocaleOverride = trimmed || null;
}

export function getEffectiveDialogLocale(getSystemLocale: () => string): string {
  return appLocaleOverride ?? getSystemLocale();
}

export function resolveDialogLocale(preferredLanguage: string): DialogLocale {
  const parts = preferredLanguage
    .trim()
    .toLowerCase()
    .replace(/_/g, "-")
    .split("-")
    .filter(Boolean);
  const lang = parts[0];
  if (lang === "zh") {
    const isTraditional = parts
      .slice(1)
      .some((sub) => sub === "hant" || sub === "tw" || sub === "hk" || sub === "mo");
    return isTraditional ? "zh-Hant" : "zh-Hans";
  }
  if (lang === "ja") return "ja";
  if (lang === "ko") return "ko";
  if (lang === "fr") return "fr";
  if (lang === "es") return "es";
  if (lang === "ru") return "ru";
  return "en";
}
