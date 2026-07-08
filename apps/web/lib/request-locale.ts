import { cache } from "react";
import { cookies, headers } from "next/headers";
import { LOCALE_COOKIE, type SupportedLocale } from "@multica/core/i18n";
import {
  isSupportedLocale,
  MULTICA_LOCALE_HEADER,
  resolveLocaleFromSignals,
} from "./locale-routing";

export const getRequestLocale = cache(
  async (): Promise<SupportedLocale> => {
    const headerList = await headers();
    const headerLocale = headerList.get(MULTICA_LOCALE_HEADER);
    if (isSupportedLocale(headerLocale)) return headerLocale;

    const cookieStore = await cookies();
    return resolveLocaleFromSignals({
      cookieLocale: cookieStore.get(LOCALE_COOKIE)?.value,
      acceptLanguage: headerList.get("accept-language"),
    });
  },
);
