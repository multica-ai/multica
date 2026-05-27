import type { Translations } from "fumadocs-ui/i18n";
import type { Lang } from "./i18n";

// Fumadocs built-in UI strings (search, TOC, last-updated, etc.) per locale.
// English uses Fumadocs defaults so we only override Chinese.
export const uiTranslations: Partial<Record<Lang, Partial<Translations>>> = {
  zh: {
    search: "搜索",
    searchNoResult: "没有找到结果",
    toc: "本页目录",
    tocNoHeadings: "无章节",
    lastUpdate: "最后更新于",
    chooseLanguage: "选择语言",
    nextPage: "下一页",
    previousPage: "上一页",
    chooseTheme: "切换主题",
    editOnGithub: "在 GitHub 上编辑",
  },
  ko: {
    search: "검색",
    searchNoResult: "결과가 없습니다",
    toc: "이 페이지에서",
    tocNoHeadings: "제목 없음",
    lastUpdate: "마지막 업데이트",
    chooseLanguage: "언어 선택",
    nextPage: "다음 페이지",
    previousPage: "이전 페이지",
    chooseTheme: "테마 변경",
    editOnGithub: "GitHub에서 편집",
  },
};

// Display name shown in the LanguageToggle dropdown.
export const localeLabels: Record<Lang, string> = {
  en: "English",
  zh: "简体中文",
  ko: "한국어",
};

// Copy for the welcome page (Hero + Byline). Pages are translated as MDX;
// this dict only carries TSX-rendered chrome above the MDX body.
export const homeCopy = {
  en: {
    eyebrow: "Multica Docs",
    titleLead: "Humans and agents,",
    titleAccent: "in one place.",
    byline: ["Getting started", "Updated April 2026", "6 min read"],
  },
  zh: {
    eyebrow: "Multica 文档",
    titleLead: "人与智能体，",
    titleAccent: "共处一方。",
    byline: ["开始使用", "2026 年 4 月更新", "阅读约 6 分钟"],
  },
  ko: {
    eyebrow: "Multica 문서",
    titleLead: "사람과 agent,",
    titleAccent: "한곳에서.",
    byline: ["시작하기", "2026년 4월 업데이트", "약 6분 읽기"],
  },
} as const satisfies Record<Lang, unknown>;
