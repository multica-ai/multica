import {
  matchLocale,
  type SupportedLocale,
} from "@multica/core/i18n";

export {
  HELPER_INSTRUCTIONS,
  HELPER_DESCRIPTION,
  type HelperInstructionsLang,
} from "./helper-instructions";
export {
  INSTALL_RUNTIME_ISSUE_TITLE,
  INSTALL_RUNTIME_ISSUE_BODY,
  FOLLOWUP_COMMENT_PREFIX,
} from "./install-runtime-issue";
export {
  CREATE_AGENT_GUIDE_ISSUE_TITLE,
  getCreateAgentGuideBody,
} from "./create-agent-guide-issue";
export {
  HELPER_STARTER_PROMPTS,
  STARTER_CARD_IDS,
  type StarterCardId,
} from "./helper-starter-prompts";
export {
  buildUserContextSection,
  type UserContextLabels,
  type QuestionnaireRaw,
} from "./user-context";

type ContentLang = "en" | "zh" | "ko" | "ja";

const CONTENT_LANG_BY_LOCALE: Record<SupportedLocale, ContentLang> = {
  en: "en",
  "zh-Hans": "zh",
  ko: "ko",
  ja: "ja",
};

/**
 * Pick persisted onboarding content for the given user language. Maps
 * supported BCP-47 prefixes to the matching variant; everything else falls
 * back to English. Mirrors the locale picker used by the frontend i18n layer.
 */
export function pickContentLang(
  language: string | null | undefined,
): ContentLang {
  return CONTENT_LANG_BY_LOCALE[matchLocale(language ? [language] : [])];
}
