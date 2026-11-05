import type { AgentGUITranscriptReader } from "../conversationServices";
import { resolveAgentGUIFirstUserMessageDisplayPrompt } from "./agentConversationTitleProjection.ts";

export type AgentGUIConversationRailTitlePromptsBySessionId = Readonly<
  Record<string, string>
>;

export function createAgentGUIConversationRailTitlePromptSelector(): (
  transcript: AgentGUITranscriptReader
) => AgentGUIConversationRailTitlePromptsBySessionId {
  let previous: AgentGUIConversationRailTitlePromptsBySessionId = {};

  return (transcript) => {
    const next: Record<string, string> = {};
    for (const agentSessionId of transcript.conversationIds()) {
      const displayPrompt = resolveAgentGUIFirstUserMessageDisplayPrompt(
        transcript.messages(agentSessionId)
      );
      if (displayPrompt) {
        next[agentSessionId] = displayPrompt;
      }
    }
    if (agentGUIConversationRailTitlePromptsEqual(previous, next)) {
      return previous;
    }
    previous = next;
    return previous;
  };
}

function agentGUIConversationRailTitlePromptsEqual(
  left: AgentGUIConversationRailTitlePromptsBySessionId,
  right: AgentGUIConversationRailTitlePromptsBySessionId
): boolean {
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  return (
    leftKeys.length === rightKeys.length &&
    leftKeys.every((key) => left[key] === right[key])
  );
}
