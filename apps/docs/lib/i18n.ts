import { defineI18n } from "fumadocs-core/i18n";

// English is the default; Chinese is available under /zh/.
// hideLocale: 'default-locale' keeps English URLs prefix-free
// (`/docs/`) while Chinese lives under `/docs/zh/...`.
// parser: 'dot' picks up `page.zh.mdx` and `meta.zh.json`.
export const i18n = defineI18n({
  languages: ["en", "zh"],
  defaultLanguage: "en",
  hideLocale: "default-locale",
  parser: "dot",
});

export type Lang = (typeof i18n.languages)[number];
