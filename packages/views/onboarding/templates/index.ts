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

/**
 * Pick the EN or ZH content for the given user language. Maps any "zh*"
 * prefix to the Chinese variant; everything else falls back to English.
 * Mirrors the server-side `noRuntimeIssueDescription` logic.
 */
export function pickContentLang(
  language: string | null | undefined,
): "en" | "zh" {
  if (language && language.toLowerCase().startsWith("zh")) return "zh";
  return "en";
}
