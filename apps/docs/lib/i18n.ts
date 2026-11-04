import { defineI18n } from "fumadocs-core/i18n";

// English is the default; Simplified Chinese (/zh/), Traditional Chinese
// (/zh-Hant/), Korean (/ko/), Japanese (/ja/), and French (/fr/) are available.
// hideLocale: 'default-locale' keeps English URLs prefix-free (`/docs/`) while
// translated locales live under `/docs/<lang>/...`.
// parser: 'dot' picks up `page.<lang>.mdx` (e.g. `page.fr.mdx`, `page.zh-Hant.mdx`)
// and `meta.<lang>.json`.
export const i18n = defineI18n({
  languages: ["en", "zh", "zh-Hant", "ko", "ja", "fr"],
  defaultLanguage: "en",
  hideLocale: "default-locale",
  parser: "dot",
});

export type Lang = (typeof i18n.languages)[number];
