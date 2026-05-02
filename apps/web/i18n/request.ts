import { cookies, headers } from "next/headers";
import { getRequestConfig } from "next-intl/server";
import { DEFAULT_LOCALE, LOCALE_COOKIE, loadMessages, normalizeLocale } from "@multica/i18n";

// Per-request locale resolution. Order:
//  1. Explicit cookie (set by user / settings UI / locale switcher).
//  2. Accept-Language header (best-effort, only matches our supported locales).
//  3. Default (zh-CN).
// URL [locale] segment will replace cookie path in PR #11.
export default getRequestConfig(async () => {
  const cookieStore = await cookies();
  const cookieValue = cookieStore.get(LOCALE_COOKIE)?.value;

  let locale = normalizeLocale(cookieValue);

  if (!cookieValue) {
    const headerStore = await headers();
    const accept = headerStore.get("accept-language") ?? "";
    if (accept.toLowerCase().startsWith("zh")) locale = "zh-CN";
    else if (accept.toLowerCase().startsWith("en")) locale = "en";
    else locale = DEFAULT_LOCALE;
  }

  return {
    locale,
    messages: loadMessages(locale) as Record<string, string>,
  };
});
