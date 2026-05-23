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
 * Pick persisted starter content for the given user language. Maps any "zh*"
 * prefix to Chinese, "tr*" to Turkish, and everything else to English.
 */
export function pickContentLang(
  language: string | null | undefined,
): "en" | "zh" | "tr" {
  const normalized = language?.toLowerCase();
  if (normalized?.startsWith("zh")) return "zh";
  if (normalized?.startsWith("tr")) return "tr";
  return "en";
}
