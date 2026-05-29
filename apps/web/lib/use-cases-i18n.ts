import type { SupportedLocale } from "@multica/core/i18n";
import { getRequestLocale } from "@/lib/request-locale";

export const getUseCaseLocale = getRequestLocale;

type UseCaseText = {
  indexTitle: string;
  indexSubtitle: string;
  indexMetadataTitle: string;
  indexMetadataDescription: string;
  cardReadMore: string;
  tableOfContents: string;
};

export const useCaseText: Record<SupportedLocale, UseCaseText> = {
  en: {
    indexTitle: "Use cases",
    indexSubtitle:
      "See how teams organize people and agents together with Multica.",
    indexMetadataTitle: "Use cases",
    indexMetadataDescription:
      "See how teams put people and agents to work together with Multica.",
    cardReadMore: "Read →",
    tableOfContents: "On this page",
  },
  "zh-Hans": {
    indexTitle: "案例",
    indexSubtitle: "看看团队怎么用 Multica 把人和 agent 一起组织起来。",
    indexMetadataTitle: "案例",
    indexMetadataDescription:
      "看看团队怎么用 Multica 把人和 agent 一起组织起来。",
    cardReadMore: "阅读 →",
    tableOfContents: "目录",
  },
};

// Secondary CTA points at the docs entry that matches the active locale,
// mirroring the convention in features/landing/i18n/zh.ts.
export function docsHrefForLocale(locale: SupportedLocale): string {
  return locale === "zh-Hans" ? "/docs/zh" : "/docs";
}
