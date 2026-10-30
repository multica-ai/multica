import type { Translations } from "fumadocs-ui/i18n";
import type { Lang } from "./i18n";

// Fumadocs built-in UI strings (search, TOC, last-updated, etc.) per locale.
// English uses Fumadocs defaults, so only the translated locales override them.
export const uiTranslations: Partial<Record<Lang, Partial<Translations>>> = {
  zh: {
    "Search(search trigger)": "搜索",
    "Search(search dialog)": "搜索",
    "No results found(search dialog)": "没有找到结果",
    "On this page(table of contents)": "本页目录",
    "No Headings(table of contents)": "无章节",
    "Last updated on(page footer)": "最后更新于",
    "Choose a language(language switcher)": "选择语言",
    "Choose a language(language switcher)(aria-label)": "选择语言",
    "Next Page(pagination)": "下一页",
    "Previous Page(pagination)": "上一页",
    "Toggle Theme(theme switcher)(aria-label)": "切换主题",
    "Edit on GitHub(edit page)": "在 GitHub 上编辑",
  },
  ko: {
    "Search(search trigger)": "검색",
    "Search(search dialog)": "검색",
    "No results found(search dialog)": "결과가 없습니다",
    "On this page(table of contents)": "이 페이지에서",
    "No Headings(table of contents)": "제목 없음",
    "Last updated on(page footer)": "마지막 업데이트",
    "Choose a language(language switcher)": "언어 선택",
    "Choose a language(language switcher)(aria-label)": "언어 선택",
    "Next Page(pagination)": "다음 페이지",
    "Previous Page(pagination)": "이전 페이지",
    "Toggle Theme(theme switcher)(aria-label)": "테마 변경",
    "Edit on GitHub(edit page)": "GitHub에서 편집",
  },
  ja: {
    "Search(search trigger)": "検索",
    "Search(search dialog)": "検索",
    "No results found(search dialog)": "結果が見つかりません",
    "On this page(table of contents)": "このページの内容",
    "No Headings(table of contents)": "見出しなし",
    "Last updated on(page footer)": "最終更新",
    "Choose a language(language switcher)": "言語を選択",
    "Choose a language(language switcher)(aria-label)": "言語を選択",
    "Next Page(pagination)": "次のページ",
    "Previous Page(pagination)": "前のページ",
    "Toggle Theme(theme switcher)(aria-label)": "テーマを変更",
    "Edit on GitHub(edit page)": "GitHub で編集",
  },
  fr: {
    "Search(search trigger)": "Rechercher",
    "Search(search dialog)": "Rechercher",
    "No results found(search dialog)": "Aucun résultat",
    "On this page(table of contents)": "Sur cette page",
    "No Headings(table of contents)": "Aucun titre",
    "Last updated on(page footer)": "Dernière mise à jour",
    "Choose a language(language switcher)": "Choisir la langue",
    "Choose a language(language switcher)(aria-label)": "Choisir la langue",
    "Next Page(pagination)": "Page suivante",
    "Previous Page(pagination)": "Page précédente",
    "Toggle Theme(theme switcher)(aria-label)": "Changer de thème",
    "Edit on GitHub(edit page)": "Modifier sur GitHub",
  },
};

// Display name shown in the LanguageToggle dropdown.
export const localeLabels: Record<Lang, string> = {
  en: "English",
  zh: "简体中文",
  ko: "한국어",
  ja: "日本語",
  fr: "Français",
};

// Copy for the welcome page (Hero + Byline). Pages are translated as MDX;
// this dict only carries TSX-rendered chrome above the MDX body.
export const homeCopy = {
  en: {
    eyebrow: "Multica Docs",
    titleLead: "Humans and agents,",
    titleAccent: "in one place.",
    byline: ["Getting started", "Updated July 2026", "2 min read"],
  },
  zh: {
    eyebrow: "Multica 文档",
    titleLead: "Multica 是人类与 AI 智能体",
    titleAccent: "共同工作的地方。",
    byline: ["开始使用", "2026 年 7 月更新", "阅读约 2 分钟"],
  },
  ko: {
    eyebrow: "Multica 문서",
    titleLead: "사람과 에이전트,",
    titleAccent: "한곳에서.",
    byline: ["시작하기", "2026년 7월 업데이트", "약 2분 분량"],
  },
  ja: {
    eyebrow: "Multica ドキュメント",
    titleLead: "人とエージェントが、",
    titleAccent: "一つの場所に。",
    byline: ["はじめに", "2026年7月更新", "約2分で読めます"],
  },
  fr: {
    eyebrow: "Documentation Multica",
    titleLead: "Humains et agents,",
    titleAccent: "au même endroit.",
    byline: ["Premiers pas", "Mis à jour en juillet 2026", "2 min de lecture"],
  },
} as const satisfies Record<Lang, unknown>;

// Chrome around the interactive demos embedded in docs pages. Each demo keeps
// its own scenario copy next to its component.
export const demoChromeCopy = {
  en: { label: "Try it", replay: "Replay", click: "Click" },
  zh: { label: "动手试试", replay: "重新演示", click: "点击" },
  ko: { label: "직접 해보기", replay: "다시 보기", click: "클릭" },
  ja: { label: "試してみる", replay: "もう一度", click: "クリック" },
  fr: { label: "Essayez", replay: "Rejouer", click: "Cliquez" },
} as const satisfies Record<Lang, unknown>;
