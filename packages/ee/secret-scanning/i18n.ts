import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import type { i18n as I18n } from "i18next";
import type { SupportedLocale } from "@multica/core/i18n";
import en from "./locales/en.json";
import es from "./locales/es.json";
import fr from "./locales/fr.json";
import ja from "./locales/ja.json";
import ko from "./locales/ko.json";
import ru from "./locales/ru.json";
import zhHans from "./locales/zh-Hans.json";
import zhHant from "./locales/zh-Hant.json";

export type SecretScanMessage = keyof typeof en;

const NAMESPACE = "ee-secret-scanning";

export const SECRET_SCAN_LOCALES: Record<SupportedLocale, Record<SecretScanMessage, string>> = {
  en,
  es,
  fr,
  ja,
  ko,
  ru,
  "zh-Hans": zhHans,
  "zh-Hant": zhHant,
};

/**
 * The app's i18n instance only carries the open-source namespaces, so this
 * package adds its own bundle the first time a component needs it.
 */
function ensureBundle(i18n: I18n): void {
  for (const lng of new Set([i18n.language, "en"])) {
    if (!lng || i18n.hasResourceBundle(lng, NAMESPACE)) continue;
    i18n.addResourceBundle(lng, NAMESPACE, SECRET_SCAN_LOCALES[lng as SupportedLocale] ?? en);
  }
}

type Translate = (key: SecretScanMessage, options?: Record<string, string | number>) => string;

export function useSecretScanT(): Translate {
  const { i18n } = useTranslation();
  ensureBundle(i18n);
  const language = i18n.language;
  return useCallback<Translate>(
    // The selector-typed t only knows the open-source namespaces.
    (key, options) => (i18n.t as unknown as (key: string, options: object) => string)(key, { ns: NAMESPACE, lng: language, ...options }),
    [i18n, language],
  );
}

/** "GitHub token" for github_token; rules the client does not know read as a secret. */
export function useRuleLabel(): (rule: string) => string {
  const t = useSecretScanT();
  return useCallback(
    (rule: string) => {
      const key = `rule_${rule}` as SecretScanMessage;
      return key in en ? t(key) : t("rule_unknown");
    },
    [t],
  );
}
