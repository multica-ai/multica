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
};

// Display name shown in the LanguageToggle dropdown.
export const localeLabels: Record<Lang, string> = {
  en: "English",
  zh: "简体中文",
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
} as const satisfies Record<Lang, unknown>;
