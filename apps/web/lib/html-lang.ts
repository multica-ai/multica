import type { SupportedLocale } from "@multica/core/i18n";

// HTML lang uses BCP-47 region tags widely recognized by screen readers and
// font stacks. i18next keeps zh-Hans internally because that is the resource
// key, while the document uses zh-CN for accessibility and CJK fallback.
export const HTML_LANG: Record<SupportedLocale, string> = {
  en: "en",
  "zh-Hans": "zh-CN",
  ko: "ko-KR",
  ja: "ja-JP",
};
