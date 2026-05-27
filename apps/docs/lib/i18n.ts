import { defineI18n } from "fumadocs-core/i18n";

// English is the default; Chinese is available under /zh/.
// hideLocale: 'default-locale' keeps English URLs prefix-free
// (`/docs/`) while translated locales live under `/docs/<lang>/...`.
// parser: 'dot' picks up `page.zh.mdx` and `meta.zh.json`.
export const i18n = defineI18n({
  languages: ["en", "zh", "ko"],
  defaultLanguage: "en",
  hideLocale: "default-locale",
  parser: "dot",
});

export type Lang = (typeof i18n.languages)[number];

// Korean docs routes are enabled before the full MDX corpus is translated.
// Until `*.ko.mdx` files exist, render the English source with Korean docs
// chrome so `/docs/ko/...` remains a stable locale URL instead of 404ing.
export function docsContentLang(lang: Lang): Lang {
  return lang === "ko" ? "en" : lang;
}
