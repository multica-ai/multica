import { defineI18n } from "fumadocs-core/i18n";

// English is the default; Chinese and Turkish are available under /zh/ and /tr/.
// hideLocale: 'default-locale' keeps English URLs prefix-free
// (`/docs/`) while other locales live under `/docs/{locale}/...`.
// parser: 'dot' picks up `page.zh.mdx`, `page.tr.mdx`, and localized meta files.
export const i18n = defineI18n({
  languages: ["en", "zh", "tr"],
  defaultLanguage: "en",
  hideLocale: "default-locale",
  parser: "dot",
});

export type Lang = (typeof i18n.languages)[number];
