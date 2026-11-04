// Agent GUI controller — prompt content normalization and optimistic messages.

import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import type {
  AgentActivityMessage
} from "@tutti-os/agent-activity-core";
import { mergeAgentGUITimelineItems } from "../model/agentGuiConversationModel";
import { projectWorkspaceAgentMessagesToTimelineItems } from "../../../shared/agentConversation/projection/workspaceAgentMessageProjection";
import { createWorkspaceAgentActivityUserMessageIdFromClientSubmitId } from "../../../shared/workspaceAgentMessageOverlay";
import type { WorkspaceAgentActivityTimelineItem } from "../../../shared/workspaceAgentTimelineTypes";
import { createAgentClientId } from "../../../shared/agentConversation/agentClientIdentity";

export function stringPayloadValue(
  value: Record<string, unknown> | undefined,
  key: string
): string | undefined {
  const nested = value?.[key];
  return typeof nested === "string" ? nested : undefined;
}

export function createAgentGUIConversationId(): string {
  return createAgentClientId();
}

export function createOptimisticPromptMessage(input: {
  workspaceId: string;
  agentSessionId: string;
  turnId: string;
  clientSubmitId?: string;
  userId: string;
  displayPrompt?: string;
  content: AgentPromptContentBlock[];
  occurredAtUnixMs: number;
}): AgentActivityMessage {
  const displayPrompt = input.displayPrompt?.trim() || undefined;
  const clientSubmitMessageId = input.clientSubmitId
    ? createWorkspaceAgentActivityUserMessageIdFromClientSubmitId(
        input.clientSubmitId
      )
    : null;
  // The echo deliberately carries no durable-domain identity: durable rows
  // use the daemon's small monotonic version counter (ADR 0004), so a fake
  // timestamp version would win every version comparison against the durable
  // twin and poison version-based cursors. version/id 0 keeps the echo out of
  // that domain; ordering comes from the durable/overlay split, not version.
  return {
    workspaceId: input.workspaceId,
    agentSessionId: input.agentSessionId,
    messageId: clientSubmitMessageId ?? `optimistic:user:${input.turnId}`,
    version: 0,
    turnId: input.turnId,
    role: "user",
    kind: "text",
    payload: {
      __agentGuiOptimisticPrompt: true,
      actorId: input.userId,
      ...(input.clientSubmitId ? { clientSubmitId: input.clientSubmitId } : {}),
      content: input.content,
      ...(displayPrompt ? { displayPrompt } : {}),
      text: displayPrompt ?? promptContentDisplayText(input.content)
    },
    occurredAtUnixMs: input.occurredAtUnixMs,
    startedAtUnixMs: input.occurredAtUnixMs
  };
}

export function projectAgentGUIMessagesToTimelineItems(
  messages: readonly AgentActivityMessage[]
): WorkspaceAgentActivityTimelineItem[] {
  return mergeAgentGUITimelineItems(
    [],
    projectWorkspaceAgentMessagesToTimelineItems(messages)
  );
}

export function normalizeOptionalText(
  value: string | null | undefined
): string | null {
  if (typeof value !== "string") {
    return null;
  }
  const trimmed = value.trim();
  return trimmed ? trimmed : null;
}

export function normalizeOptionalPrompt(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

export function textPromptContent(prompt: string): AgentPromptContentBlock[] {
  const text = prompt.trim();
  return text ? [{ type: "text", text }] : [];
}

export function normalizePromptContentBlocks(
  content: readonly AgentPromptContentBlock[]
): AgentPromptContentBlock[] {
  const result: AgentPromptContentBlock[] = [];
  for (const block of content) {
    if (block.type === "text") {
      const text = block.text?.trim() ?? "";
      if (text) {
        result.push({ type: "text", text });
      }
      continue;
    }
    if (block.type === "image") {
      const mimeType = block.mimeType?.trim();
      const data = block.data?.trim();
      const url = block.url?.trim();
      if (
        (!data && !url) ||
        (data && url) ||
        (url && !isSafePromptImageUrl(url)) ||
        (mimeType !== "image/png" &&
          mimeType !== "image/jpeg" &&
          mimeType !== "image/webp")
      ) {
        continue;
      }
      result.push({
        type: "image",
        mimeType,
        ...(url ? { url } : { data }),
        ...(block.attachmentId?.trim()
          ? { attachmentId: block.attachmentId.trim() }
          : {}),
        ...(block.name?.trim() ? { name: block.name.trim() } : {})
      });
      continue;
    }
    if (block.type === "skill" || block.type === "mention") {
      const name = block.name?.trim();
      const path = block.path?.trim();
      if (name && path) {
        result.push({ type: block.type, name, path });
      }
      continue;
    }
    if (block.type === "connector") {
      const connectorKey = block.connectorKey?.trim();
      if (connectorKey && /^[a-z][a-z0-9._-]{0,127}$/.test(connectorKey)) {
        result.push({ type: "connector", connectorKey });
      }
    }
  }
  return result;
}

function isSafePromptImageUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return (
      url.protocol === "https:" &&
      Boolean(url.hostname) &&
      !url.username &&
      !url.password
    );
  } catch {
    return false;
  }
}

export function promptContentDisplayText(
  content: readonly AgentPromptContentBlock[]
): string {
  return content
    .filter((block) => block.type === "text")
    .map((block) => block.text?.trim() ?? "")
    .filter(Boolean)
    .join("\n");
}

export function promptContentHasImage(
  content: readonly AgentPromptContentBlock[]
): boolean {
  return content.some((block) => block.type === "image");
}

export function recordValue(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}
