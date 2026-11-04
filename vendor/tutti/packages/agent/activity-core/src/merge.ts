import type { AgentActivityMessage } from "./types.ts";

export function mergeAgentActivityMessages<
  T extends AgentActivityMessage = AgentActivityMessage
>(currentMessages: readonly T[], incomingMessages: readonly T[]): T[] {
  if (incomingMessages.length === 0) {
    return [...currentMessages];
  }

  // Messages are not changed in place, so an unchanged one keeps its object, and
  // what is built from it can be kept too.
  const byMessageId = new Map<string, T>();
  for (const message of currentMessages) {
    byMessageId.set(message.messageId, message);
  }

  for (const incoming of incomingMessages) {
    const existing = byMessageId.get(incoming.messageId);
    if (existing === incoming) continue;
    if (!existing || shouldReplaceAgentActivityMessage(existing, incoming)) {
      byMessageId.set(
        incoming.messageId,
        existing ? mergeAgentActivityMessage(existing, incoming) : incoming
      );
    }
  }

  return [...byMessageId.values()].sort(compareAgentActivityMessages);
}

export function compareAgentActivityMessages(
  left: AgentActivityMessage,
  right: AgentActivityMessage
): number {
  const leftSequence = positiveNumber(left.sequence);
  const rightSequence = positiveNumber(right.sequence);
  if (leftSequence > 0 && rightSequence > 0 && leftSequence !== rightSequence) {
    return leftSequence - rightSequence;
  }
  return (
    left.occurredAtUnixMs - right.occurredAtUnixMs ||
    left.version - right.version ||
    left.messageId.localeCompare(right.messageId)
  );
}

export function latestAgentActivityMessageVersion(
  messages: readonly AgentActivityMessage[]
): number {
  return messages.reduce(
    (latestVersion, message) => Math.max(latestVersion, message.version),
    0
  );
}

export function areAgentActivityMessageArraysEqual(
  left: readonly AgentActivityMessage[],
  right: readonly AgentActivityMessage[]
): boolean {
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (!areAgentActivityMessagesEqual(left[index]!, right[index]!)) {
      return false;
    }
  }
  return true;
}

export function areAgentActivityMessagesEqual(
  left: AgentActivityMessage,
  right: AgentActivityMessage
): boolean {
  if (left === right) {
    return true;
  }
  return (
    left.workspaceId === right.workspaceId &&
    left.agentSessionId === right.agentSessionId &&
    left.messageId === right.messageId &&
    left.version === right.version &&
    Object.is(left.turnId, right.turnId) &&
    left.role === right.role &&
    left.kind === right.kind &&
    Object.is(left.status, right.status) &&
    areJsonLikeValuesEqual(left.semantics, right.semantics) &&
    Object.is(left.sequence, right.sequence) &&
    Object.is(left.occurredAtUnixMs, right.occurredAtUnixMs) &&
    Object.is(left.createdAtUnixMs, right.createdAtUnixMs) &&
    Object.is(left.startedAtUnixMs, right.startedAtUnixMs) &&
    Object.is(left.completedAtUnixMs, right.completedAtUnixMs) &&
    areJsonLikeValuesEqual(left.payload, right.payload)
  );
}

function positiveNumber(value: number | undefined): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? value
    : 0;
}

export function cloneAgentActivityMessage<
  T extends AgentActivityMessage = AgentActivityMessage
>(message: T): T {
  return {
    ...message,
    semantics: message.semantics ? { ...message.semantics } : undefined,
    payload: { ...message.payload }
  };
}

function mergeAgentActivityMessage<T extends AgentActivityMessage>(
  existing: T,
  incoming: T
): T {
  return {
    ...existing,
    ...incoming,
    sequence: incoming.sequence ?? existing.sequence,
    createdAtUnixMs: incoming.createdAtUnixMs ?? existing.createdAtUnixMs,
    semantics: incoming.semantics
      ? {
          ...existing.semantics,
          ...incoming.semantics
        }
      : existing.semantics,
    payload: {
      ...existing.payload,
      ...incoming.payload
    }
  };
}

function shouldReplaceAgentActivityMessage(
  existing: AgentActivityMessage,
  incoming: AgentActivityMessage
): boolean {
  if (incoming.version !== existing.version) {
    return incoming.version > existing.version;
  }
  return incoming.occurredAtUnixMs >= existing.occurredAtUnixMs;
}

export function areJsonLikeValuesEqual(left: unknown, right: unknown): boolean {
  if (Object.is(left, right)) {
    return true;
  }
  if (Array.isArray(left) || Array.isArray(right)) {
    if (!Array.isArray(left) || !Array.isArray(right)) {
      return false;
    }
    if (left.length !== right.length) {
      return false;
    }
    for (let index = 0; index < left.length; index += 1) {
      if (!areJsonLikeValuesEqual(left[index], right[index])) {
        return false;
      }
    }
    return true;
  }
  const leftRecord = recordValue(left);
  const rightRecord = recordValue(right);
  if (!leftRecord || !rightRecord) {
    return false;
  }
  const keys = new Set([
    ...Object.keys(leftRecord),
    ...Object.keys(rightRecord)
  ]);
  for (const key of keys) {
    if (!areJsonLikeValuesEqual(leftRecord[key], rightRecord[key])) {
      return false;
    }
  }
  return true;
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : null;
}
