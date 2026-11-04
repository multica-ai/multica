import type {
  AgentActivityMessage,
  AgentActivitySession
} from "@tutti-os/agent-activity-core";
import { translate } from "../i18n/index";
import { normalizeAgentTitleText } from "./utils/agentTitleText";
import { fallbackSummary } from "./workspaceAgentLatestActivitySummary";
import { isWorkspaceAgentSyntheticControlMessage } from "./workspaceAgentSyntheticMessages";
import type { WorkspaceAgentActivityStatus } from "./workspaceAgentActivityListTypes";

export function resolveWorkspaceAgentActivityTitle(
  session: AgentActivitySession
): string {
  return session.title.trim() || workspaceAgentUntitledConversationLabel();
}

export function resolveLatestActivity(
  messages: readonly AgentActivityMessage[],
  status: WorkspaceAgentActivityStatus,
  actors: { agentName: string; userName: string }
): { actorName: string; summary: string } {
  const latestMessage = latestDisplayableMessage(messages);
  if (latestMessage) {
    return {
      actorName:
        messageRole(latestMessage.message) === "user"
          ? actors.userName
          : actors.agentName,
      summary: latestMessage.text
    };
  }
  return {
    actorName: actors.agentName,
    summary: fallbackSummary(status)
  };
}

function latestDisplayableMessage(messages: readonly AgentActivityMessage[]): {
  message: AgentActivityMessage;
  text: string;
  time?: number;
} | null {
  // Reading a message's text is the expensive part, so ask for it only when the
  // message would become the latest. Newest first: nearly all are ruled out by time.
  let latest: { message: AgentActivityMessage; text: string; time?: number } | null = null;
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index]!;
    if (normalizeToken(message.kind) === "tool_call") continue;
    const time = messageTime(message);
    if (latest && !isLaterMessage(message, time, latest.message, latest.time)) continue;
    const text = messageDisplayText(message);
    if (text.length > 0) latest = { message, text, time };
  }
  return latest;
}

function isLaterMessage(
  candidate: AgentActivityMessage,
  candidateTime: number | undefined,
  current: AgentActivityMessage,
  currentTime: number | undefined
): boolean {
  const timeDiff = (candidateTime ?? 0) - (currentTime ?? 0);
  return timeDiff !== 0
    ? timeDiff > 0
    : candidate.messageId.localeCompare(current.messageId) > 0;
}

function messageDisplayText(message: AgentActivityMessage): string {
  const payload = message.payload ?? {};
  const text = normalizeAgentTitleText(
    compactText(
      stringValue(payload.displayPrompt) ||
        stringValue(payload.text) ||
        stringValue(payload.content) ||
        stringValue(payload.message) ||
        stringValue(payload.body) ||
        stringValue(payload.title) ||
        ""
    )
  );
  return isWorkspaceAgentSyntheticControlMessage(text) ? "" : text;
}

function messageRole(message: AgentActivityMessage): string {
  return normalizeToken(message.role);
}

function messageTime(message: AgentActivityMessage): number | undefined {
  return (
    message.occurredAtUnixMs ??
    message.completedAtUnixMs ??
    message.startedAtUnixMs
  );
}

function normalizeToken(value: string | undefined): string {
  return value?.trim().toLowerCase() ?? "";
}

function compactText(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function workspaceAgentUntitledConversationLabel(): string {
  return normalizeAgentTitleText(
    translate("agentHost.workspaceAgentsUntitledConversation")
  );
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}
