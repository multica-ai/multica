import type { AgentPromptContentBlock } from "@tutti-os/agent-activity-core";
import { extractRichTextMentionsFromContent } from "@tutti-os/ui-rich-text/core";
import { MULTICA_GUI_CAPABILITIES } from "./session-metadata";

export function promptHasAgentMention(
  content: readonly AgentPromptContentBlock[]
): boolean {
  return content.some(
    (block) =>
      block.type === "text" &&
      block.text &&
      extractRichTextMentionsFromContent(block.text).some(
        (mention) => mention.providerId === "agent-target"
      )
  );
}

export function promptSubmissionError(
  input: {
    guidance?: boolean;
    targetTurnId?: string | null;
    capabilityRefs?: readonly unknown[] | null;
  },
  locale: string
): string | null {
  if (
    (input.guidance || input.targetTurnId) &&
    !MULTICA_GUI_CAPABILITIES.activeTurnGuidance
  ) {
    return locale === "zh-CN"
      ? "当前连接不支持引导当前回复。请使用普通发送排队，或先提交当前问题的回答。"
      : "This connection cannot guide the current reply. Use normal send to queue your message, or answer the pending question first.";
  }
  if (input.capabilityRefs?.length) {
    return locale === "zh-CN"
      ? "当前连接不支持此能力引用，请移除后重新发送。"
      : "This connection does not support capability references. Remove them before sending again.";
  }
  return null;
}
