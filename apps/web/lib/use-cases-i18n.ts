import { cookies, headers } from "next/headers";
import { LOCALE_COOKIE } from "@multica/core/i18n";
import type { Locale } from "@/features/landing/i18n";

// Mirrors `LandingLayout.getInitialLocale` (apps/web/app/(landing)/layout.tsx):
// 1. read the `multica-locale` cookie written by the landing footer's
//    language toggle, 2. fall back to Accept-Language, 3. default to English.
// Keeping these two server-side locale resolvers in lockstep is what makes
// the cookie-based locale strategy coherent across the landing pages and the
// use-case routes — drift here is exactly how the header/footer would say
// one thing while the use-case page rendered the other.
export async function getUseCaseLocale(): Promise<Locale> {
  const cookieStore = await cookies();
  const stored = cookieStore.get(LOCALE_COOKIE)?.value;
  if (stored === "en" || stored === "zh") return stored;

  const headerList = await headers();
  const acceptLang = headerList.get("accept-language") ?? "";
  if (acceptLang.includes("zh")) return "zh";

  return "en";
}

type UseCaseText = {
  indexTitle: string;
  indexSubtitle: string;
  indexMetadataTitle: string;
  indexMetadataDescription: string;
  cardReadMore: string;
  tableOfContents: string;
};

export const useCaseText: Record<Locale, UseCaseText> = {
  zh: {
    indexTitle: "案例",
    indexSubtitle: "看看团队怎么用 Multica 把人和 agent 一起组织起来。",
    indexMetadataTitle: "案例 | Multica",
    indexMetadataDescription:
      "看看团队怎么用 Multica 把人和 agent 一起组织起来。",
    cardReadMore: "阅读 →",
    tableOfContents: "目录",
  },
  en: {
    indexTitle: "Use cases",
    indexSubtitle:
      "See how teams organize people and agents together with Multica.",
    indexMetadataTitle: "Use cases | Multica",
    indexMetadataDescription:
      "See how teams put people and agents to work together with Multica.",
    cardReadMore: "Read →",
    tableOfContents: "On this page",
  },
};

// Secondary CTA points at the docs entry that matches the active locale,
// mirroring the convention in features/landing/i18n/zh.ts and
// how-it-works-section: zh → /docs/zh, en → /docs.
export function docsHrefForLocale(locale: Locale): string {
  return locale === "zh" ? "/docs/zh" : "/docs";
}
