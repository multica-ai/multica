import type { AgentActivitySessionDetailSnapshot } from "./engine/sessionReconcile.types.ts";
import type {
  AgentActivityMessage,
  AgentActivityMessagePage,
  AgentActivitySession
} from "./types.ts";

export type ReconcileSessionIdentity = Pick<
  AgentActivitySession,
  "agentSessionId" | "kind" | "messageVersion"
>;

export function withoutDeletedChildren(
  detail: AgentActivitySessionDetailSnapshot,
  isSessionDeleted: (agentSessionId: string) => boolean
): AgentActivitySessionDetailSnapshot {
  const removedIds = new Set(
    detail.childSessions
      .filter((session) => isSessionDeleted(session.agentSessionId))
      .map((session) => session.agentSessionId)
  );
  for (;;) {
    let removedDescendant = false;
    for (const session of detail.childSessions) {
      if (
        !removedIds.has(session.agentSessionId) &&
        session.parentAgentSessionId !== null &&
        removedIds.has(session.parentAgentSessionId)
      ) {
        removedIds.add(session.agentSessionId);
        removedDescendant = true;
      }
    }
    if (!removedDescendant) break;
  }
  const childSessions = detail.childSessions.filter(
    (session) => !removedIds.has(session.agentSessionId)
  );
  const retainedIds = new Set([
    detail.session.agentSessionId,
    ...childSessions.map((session) => session.agentSessionId)
  ]);
  return {
    projection: detail.projection,
    lifecycleCapabilitiesProjected: detail.lifecycleCapabilitiesProjected,
    session: detail.session,
    childSessions,
    historyRevision: detail.historyRevision,
    turns: detail.turns.filter((turn) => retainedIds.has(turn.agentSessionId))
  };
}

export function assertMessagePageIdentity(
  page: AgentActivityMessagePage,
  workspaceId: string,
  agentSessionId: string
): void {
  for (const message of page.messages) {
    if (message.agentSessionId.trim() !== agentSessionId) {
      throw new Error(
        `session reconcile message identity mismatch: expected ${agentSessionId}, received ${message.agentSessionId}`
      );
    }
    if (
      message.workspaceId !== undefined &&
      message.workspaceId.trim() !== workspaceId
    ) {
      throw new Error(
        `session reconcile message workspace mismatch: expected ${workspaceId}, received ${message.workspaceId}`
      );
    }
  }
}

export function shouldRetrySessionDetailRead(error: unknown): boolean {
  if (!error || typeof error !== "object") return true;
  const statusCode = (error as { statusCode?: unknown }).statusCode;
  return !(
    typeof statusCode === "number" &&
    Number.isSafeInteger(statusCode) &&
    statusCode >= 400 &&
    statusCode < 500
  );
}

export function detailSessionIds(
  detail: AgentActivitySessionDetailSnapshot
): string[] {
  return [
    detail.session.agentSessionId,
    ...detail.childSessions.map((session) => session.agentSessionId)
  ];
}

export function latestMessageVersion(
  messages: readonly AgentActivityMessage[]
): number {
  return messages.reduce(
    (latest, message) => Math.max(latest, normalizeVersion(message.version)),
    0
  );
}

export function latestDurableMessageVersion(
  messages: readonly AgentActivityMessage[]
): number {
  return messages.reduce((latest, message) => {
    if (
      !Number.isSafeInteger(message.sequence) ||
      (message.sequence ?? 0) <= 0 ||
      !Number.isSafeInteger(message.version) ||
      message.version <= 0
    ) {
      return latest;
    }
    return Math.max(latest, message.version);
  }, 0);
}

export function conversationReconcileAfterVersion(
  messages: readonly AgentActivityMessage[]
): number {
  const latest = latestMessageVersion(messages);
  if (
    messages.length === 0 ||
    messages.some((message) => message.role.trim().toLowerCase() === "user")
  ) {
    return latest;
  }
  return messages.some((message) => {
    const role = message.role.trim().toLowerCase();
    const kind = message.kind.trim().toLowerCase();
    return role === "assistant" || role === "agent" || kind === "tool_call";
  })
    ? 0
    : latest;
}

export function normalizeVersion(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.trunc(value)) : 0;
}

export function normalizeRequiredIdentity(
  value: string,
  field: string
): string {
  const normalized = value.trim();
  if (!normalized) throw new Error(`session reconcile ${field} is required`);
  return normalized;
}

export function fallbackSession(
  agentSessionId: string
): ReconcileSessionIdentity {
  return {
    agentSessionId,
    kind: "root",
    messageVersion: 0
  };
}
