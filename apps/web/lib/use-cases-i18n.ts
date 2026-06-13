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
  ja: {
    indexTitle: "ユースケース",
    indexSubtitle:
      "チームが Multica で人とエージェントをどう組み合わせて動かしているかをご覧ください。",
    indexMetadataTitle: "ユースケース",
    indexMetadataDescription:
      "チームが Multica で人とエージェントを一緒に働かせる方法をご覧ください。",
    cardReadMore: "続きを読む →",
    tableOfContents: "このページの内容",
  },
  vi: {
    indexTitle: "Tình huống sử dụng",
    indexSubtitle:
      "Xem cách các đội nhóm tổ chức con người và agent làm việc cùng nhau với Hira.",
    indexMetadataTitle: "Tình huống sử dụng",
    indexMetadataDescription:
      "Xem cách các đội nhóm đưa con người và agent vào làm việc cùng nhau với Hira.",
    cardReadMore: "Đọc tiếp →",
    tableOfContents: "Trong trang này",
  },
};
