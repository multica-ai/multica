import { defineI18n } from "fumadocs-core/i18n";

// English is the default; Chinese (/zh/), Korean (/ko/), and Japanese (/ja/)
// are available. hideLocale: 'default-locale' keeps English URLs prefix-free
// (`/docs/`) while translated locales live under `/docs/<lang>/...`.
// parser: 'dot' picks up `page.zh.mdx` / `page.ko.mdx` / `page.ja.mdx` and `meta.<lang>.json`.
export const i18n = defineI18n({
  languages: ["en", "zh", "ko", "ja"],
  defaultLanguage: "en",
  hideLocale: "default-locale",
  parser: "dot",
});

export type Lang = (typeof i18n.languages)[number];
