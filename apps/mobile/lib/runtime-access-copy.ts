import { i18n } from "./i18n/singleton";

/**
 * Localized copy helpers for runtime ownership rejections on mobile.
 * Backed by the mobile `chat` and `issues` locale bundles so non-English
 * builds never fall back to hardcoded English strings.
 */
export function runtimeAccessDeniedRecoveryCopy(
  t: (key: string) => string = i18n.getFixedT(i18n.language, "chat"),
): string {
  return t("failure.runtime_access_recovery");
}

export function runtimeAccessDeniedBadge(
  t: (key: string) => string = i18n.getFixedT(i18n.language, "issues"),
): string {
  return t("runs.failure_badge.runtime_access_denied");
}
