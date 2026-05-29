import type { SupportedLocale } from "@multica/core/i18n";
export { docsHrefForLocale } from "@/lib/docs-href";
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
  ko: {
    indexTitle: "사용 사례",
    indexSubtitle:
      "팀이 Multica로 사람과 에이전트를 함께 구성하는 방법을 확인해 보세요.",
    indexMetadataTitle: "사용 사례",
    indexMetadataDescription:
      "팀이 Multica로 사람과 에이전트를 함께 일하게 만드는 방법을 확인해 보세요.",
    cardReadMore: "읽기 →",
    tableOfContents: "이 페이지에서",
  },
};
