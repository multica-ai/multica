import { defineI18n } from "fumadocs-core/i18n";

// English is the default; Chinese (/zh/), Korean (/ko/), Japanese (/ja/), and
// French (/fr/) are available. hideLocale: 'default-locale' keeps English URLs
// prefix-free (`/docs/`) while translated locales live under `/docs/<lang>/...`.
// parser: 'dot' picks up `page.<lang>.mdx` (e.g. `page.fr.mdx`) and `meta.<lang>.json`.
export const i18n = defineI18n({
  languages: ["en", "zh", "ko", "ja", "fr"],
  defaultLanguage: "en",
  hideLocale: "default-locale",
  parser: "dot",
});

export type Lang = (typeof i18n.languages)[number];
