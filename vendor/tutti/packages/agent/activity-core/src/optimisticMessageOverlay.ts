import { cloneJSONValue } from "./activityValueParsing.ts";
import type {
  AgentActivityLiveEvent,
  AgentActivityMessageDeltaEvent
} from "./liveEvent.types.ts";
import {
  cloneAgentActivityMessage,
  compareAgentActivityMessages
} from "./merge.ts";
import type {
  AgentActivityMessage,
  AgentActivityMessageSemantics,
  AgentActivityTurn
} from "./types.ts";

export interface AgentActivityOptimisticApplyResult {
  applied: boolean;
  needsReconcile: boolean;
  reason?:
    | "append_without_anchor"
    | "identity_mismatch"
    | "late_after_terminal"
    | "tool_output_offset_mismatch";
}

export interface AgentActivityOptimisticMessageScope {
  workspaceId: string;
  agentSessionId: string;
}

export interface AgentActivityOptimisticMessageOverlay {
  apply(event: AgentActivityLiveEvent): AgentActivityOptimisticApplyResult;
  /** Returns one already-applied optimistic message without projecting its Session history. */
  getOptimisticMessage(
    scope: AgentActivityOptimisticMessageScope,
    messageId: string
  ): AgentActivityMessage | null;
  /**
   * Replaces the authoritative base for one Session after a successful read.
   *
   * Ordinary optimistic projections are discarded because the read is now the
   * best known truth. An explicitly terminal optimistic projection remains
   * visible over a nonterminal canonical message until canonical truth also
   * reaches a terminal state.
   */
  reconcile(
    scope: AgentActivityOptimisticMessageScope,
    canonicalMessages: readonly AgentActivityMessage[]
  ): void;
  /**
   * Replaces one Session from an authoritative effective-history snapshot.
   *
   * Unlike an ordinary read, omission is meaningful after history replacement:
   * a terminal optimistic row is removed when neither its Turn nor stable
   * client-submit identity remains in effective history. Turnless controls stay
   * projected until another explicit lifecycle boundary removes the Session.
   */
  reconcileAuthoritativeHistory(
    scope: AgentActivityOptimisticMessageScope,
    canonicalMessages: readonly AgentActivityMessage[],
    effectiveTurns: readonly AgentActivityTurn[]
  ): void;
  /**
   * Drops all cached state for one Session when the host removes or rebinds
   * that Session. A stream discontinuity should instead complete an
   * authoritative read and call reconcile so a confirmed optimistic terminal
   * projection remains visible until canonical terminal truth arrives.
   */
  reset(scope: AgentActivityOptimisticMessageScope): void;
  project(
    scope: AgentActivityOptimisticMessageScope,
    canonicalMessages: readonly AgentActivityMessage[]
  ): AgentActivityMessage[];
}

interface OptimisticEntry {
  message: AgentActivityMessage;
  payloadUnset: ReadonlySet<string>;
  explicitlyTerminal: boolean;
}

interface ProjectionCacheEntry {
  canonicalMessages: readonly AgentActivityMessage[];
  projectedMessages: AgentActivityMessage[];
  revision: number;
}

export function createAgentActivityOptimisticMessageOverlay(): AgentActivityOptimisticMessageOverlay {
  const optimistic = new Map<string, OptimisticEntry>();
  const canonical = new Map<string, AgentActivityMessage>();
  const projectionCache = new Map<string, ProjectionCacheEntry>();
  const scopeRevisions = new Map<string, number>();

  function replaceCanonicalBase(
    scope: AgentActivityOptimisticMessageScope,
    messages: readonly AgentActivityMessage[]
  ): {
    normalized: AgentActivityMessage[];
    prefix: string;
  } {
    const normalized = normalizeCanonicalMessages(scope, messages);
    const prefix = scopePrefix(scope);
    deleteScopeEntries(canonical, prefix);
    for (const message of normalized) {
      canonical.set(messageKey(message), cloneMessage(message));
    }
    return { normalized, prefix };
  }

  return {
    apply(event) {
      if (event.eventType !== "message_delta") {
        return { applied: false, needsReconcile: false };
      }
      const result = applyMessageDelta(event);
      if (result.applied) {
        markScopeChanged(event);
      }
      return result;
    },

    getOptimisticMessage(scope, messageId) {
      const key = `${scopePrefix(scope)}${messageId}`;
      const entry = optimistic.get(key);
      if (!entry) return null;
      const base = canonical.get(key);
      return base
        ? materialize(base, entry.message, entry.payloadUnset)
        : cloneMessage(entry.message);
    },

    reconcile(scope, messages) {
      const { prefix } = replaceCanonicalBase(scope, messages);
      for (const [key, entry] of optimistic) {
        if (!key.startsWith(prefix)) {
          continue;
        }
        const canonicalMessage = canonical.get(key);
        if (
          !entry.explicitlyTerminal ||
          (canonicalMessage !== undefined &&
            isTerminalMessage(canonicalMessage))
        ) {
          optimistic.delete(key);
        }
      }
      markScopeChanged(scope);
    },

    reconcileAuthoritativeHistory(scope, messages, effectiveTurns) {
      const { normalized, prefix } = replaceCanonicalBase(scope, messages);
      const effectiveTurnIds = new Set<string>();
      for (const turn of effectiveTurns) {
        if (turn.agentSessionId !== scope.agentSessionId) {
          throw new Error(
            "effective Agent activity Turn is outside the optimistic overlay scope"
          );
        }
        const turnId = turn.turnId.trim();
        if (turnId) effectiveTurnIds.add(turnId);
      }
      const effectiveClientSubmitIds = new Set(
        normalized
          .map(clientSubmitId)
          .filter((value): value is string => value !== null)
      );
      for (const [key, entry] of optimistic) {
        if (!key.startsWith(prefix)) continue;
        const canonicalMessage = canonical.get(key);
        if (
          !entry.explicitlyTerminal ||
          (canonicalMessage !== undefined &&
            isTerminalMessage(canonicalMessage))
        ) {
          optimistic.delete(key);
          continue;
        }
        const turnId = entry.message.turnId?.trim() ?? "";
        if (!turnId) continue;
        const submitId = clientSubmitId(entry.message);
        if (
          !effectiveTurnIds.has(turnId) &&
          (submitId === null || !effectiveClientSubmitIds.has(submitId))
        ) {
          optimistic.delete(key);
        }
      }
      markScopeChanged(scope);
    },

    reset(scope) {
      const prefix = scopePrefix(scope);
      deleteScopeEntries(optimistic, prefix);
      deleteScopeEntries(canonical, prefix);
      markScopeChanged(scope);
    },

    project(scope, messages) {
      const prefix = scopePrefix(scope);
      const revision = scopeRevisions.get(prefix) ?? 0;
      const cached = projectionCache.get(prefix);
      if (
        cached?.canonicalMessages === messages &&
        cached.revision === revision
      ) {
        return cached.projectedMessages;
      }
      const normalized = normalizeCanonicalMessages(scope, messages);
      const byKey = new Map<string, AgentActivityMessage>();
      for (const message of normalized) {
        byKey.set(messageKey(message), cloneMessage(message));
      }
      for (const [key, entry] of optimistic) {
        if (!key.startsWith(prefix)) {
          continue;
        }
        const base = byKey.get(key) ?? canonical.get(key);
        byKey.set(
          key,
          base
            ? materialize(base, entry.message, entry.payloadUnset)
            : cloneMessage(entry.message)
        );
      }
      const projectedMessages = [...byKey.values()].sort(
        compareAgentActivityMessages
      );
      projectionCache.set(prefix, {
        canonicalMessages: messages,
        projectedMessages,
        revision
      });
      return projectedMessages;
    }
  };

  function markScopeChanged(scope: AgentActivityOptimisticMessageScope): void {
    const prefix = scopePrefix(scope);
    scopeRevisions.set(prefix, (scopeRevisions.get(prefix) ?? 0) + 1);
    projectionCache.delete(prefix);
  }

  function applyMessageDelta(
    event: AgentActivityMessageDeltaEvent
  ): AgentActivityOptimisticApplyResult {
    const data = event.data;
    if (
      data.workspaceId !== event.workspaceId ||
      data.agentSessionId !== event.agentSessionId
    ) {
      return {
        applied: false,
        needsReconcile: true,
        reason: "identity_mismatch"
      };
    }
    const key = liveMessageKey(event);
    const existing =
      optimistic.get(key)?.message ?? canonical.get(key) ?? undefined;
    if (
      existing &&
      isTerminalMessage(existing) &&
      !deltaIsExplicitlyTerminal(data)
    ) {
      return {
        applied: false,
        needsReconcile: true,
        reason: "late_after_terminal"
      };
    }
    if (data.content?.operation === "append_text" && !existing) {
      return {
        applied: false,
        needsReconcile: true,
        reason: "append_without_anchor"
      };
    }
    if (data.toolOutput?.operation === "append_text" && !existing) {
      return {
        applied: false,
        needsReconcile: true,
        reason: "append_without_anchor"
      };
    }

    const next: AgentActivityMessage = existing
      ? cloneMessage(existing)
      : {
          workspaceId: event.workspaceId,
          agentSessionId: event.agentSessionId,
          messageId: data.messageId,
          version: 0,
          turnId: data.turnId ?? null,
          role: data.role,
          kind: data.kind,
          payload: {},
          occurredAtUnixMs: data.occurredAtUnixMs
        };
    next.workspaceId = event.workspaceId;
    next.agentSessionId = event.agentSessionId;
    next.turnId = data.turnId ?? next.turnId;
    next.role = data.role;
    next.kind = data.kind;
    next.occurredAtUnixMs = Math.max(
      next.occurredAtUnixMs,
      data.occurredAtUnixMs
    );

    if (data.content?.operation === "append_text") {
      const currentText =
        typeof next.payload.text === "string" ? next.payload.text : "";
      const text = currentText + data.content.text;
      next.payload.text = text;
      next.payload.content = text;
    } else if (data.content?.operation === "set") {
      const value = cloneJSONValue(data.content.value);
      next.payload.content = value;
      if (typeof value === "string") {
        next.payload.text = value;
      } else {
        delete next.payload.text;
      }
    }
    if (data.toolOutput) {
      const output = recordValue(next.payload.output) ?? {};
      const currentText = typeof output.text === "string" ? output.text : "";
      if (
        data.toolOutput.operation === "append_text" &&
        utf8ByteLength(currentText) !== data.toolOutput.offsetBytes
      ) {
        return {
          applied: false,
          needsReconcile: true,
          reason: "tool_output_offset_mismatch"
        };
      }
      output.text =
        data.toolOutput.operation === "set"
          ? data.toolOutput.text
          : currentText + data.toolOutput.text;
      next.payload.output = output;
    }
    for (const [payloadKey, value] of Object.entries(data.payloadSet ?? {})) {
      next.payload[payloadKey] = cloneJSONValue(value);
    }
    for (const payloadKey of data.payloadUnset ?? []) {
      delete next.payload[payloadKey];
    }
    if (data.status !== undefined) next.status = data.status;
    if (data.semantics !== undefined) {
      next.semantics = cloneJSONValue(
        data.semantics
      ) as AgentActivityMessageSemantics;
    }
    if (data.startedAtUnixMs !== undefined) {
      next.startedAtUnixMs = data.startedAtUnixMs;
    }
    if (data.completedAtUnixMs !== undefined) {
      next.completedAtUnixMs = data.completedAtUnixMs;
    }
    const payloadUnset = new Set(optimistic.get(key)?.payloadUnset ?? []);
    for (const payloadKey of Object.keys(data.payloadSet ?? {})) {
      payloadUnset.delete(payloadKey);
    }
    for (const payloadKey of data.payloadUnset ?? []) {
      payloadUnset.add(payloadKey);
    }
    optimistic.set(key, {
      message: next,
      payloadUnset,
      explicitlyTerminal:
        (optimistic.get(key)?.explicitlyTerminal ?? false) ||
        deltaIsExplicitlyTerminal(data)
    });
    return { applied: true, needsReconcile: false };
  }
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (cloneJSONValue(value) as Record<string, unknown>)
    : null;
}

function clientSubmitId(message: AgentActivityMessage): string | null {
  const value = message.payload.clientSubmitId;
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

function materialize(
  canonical: AgentActivityMessage,
  overlay: AgentActivityMessage,
  payloadUnset: ReadonlySet<string>
): AgentActivityMessage {
  const materialized = {
    ...canonical,
    ...overlay,
    version: canonical.version,
    sequence: canonical.sequence ?? overlay.sequence,
    createdAtUnixMs: canonical.createdAtUnixMs ?? overlay.createdAtUnixMs,
    payload: {
      ...canonical.payload,
      ...overlay.payload
    },
    semantics: overlay.semantics ?? canonical.semantics
  };
  for (const key of payloadUnset) {
    delete materialized.payload[key];
  }
  return materialized;
}

function isTerminalMessage(message: AgentActivityMessage): boolean {
  if (message.completedAtUnixMs !== undefined) return true;
  return isTerminalStatus(message.status);
}

function deltaIsExplicitlyTerminal(
  data: AgentActivityMessageDeltaEvent["data"]
): boolean {
  return data.completedAtUnixMs !== undefined || isTerminalStatus(data.status);
}

function isTerminalStatus(status: string | null | undefined): boolean {
  return ["completed", "failed", "canceled", "interrupted"].includes(
    status ?? ""
  );
}

function liveMessageKey(event: AgentActivityMessageDeltaEvent): string {
  return `${identityPrefix(event.workspaceId, event.agentSessionId)}${event.data.messageId}`;
}

function messageKey(message: AgentActivityMessage): string {
  return `${identityPrefix(message.workspaceId ?? "", message.agentSessionId)}${message.messageId}`;
}

function identityPrefix(workspaceId: string, agentSessionId: string): string {
  return `${workspaceId}\u0000${agentSessionId}\u0000`;
}

function scopePrefix(scope: AgentActivityOptimisticMessageScope): string {
  return identityPrefix(scope.workspaceId, scope.agentSessionId);
}

function normalizeCanonicalMessages(
  scope: AgentActivityOptimisticMessageScope,
  messages: readonly AgentActivityMessage[]
): AgentActivityMessage[] {
  return messages.map((message) => {
    if (
      (message.workspaceId !== undefined &&
        message.workspaceId !== scope.workspaceId) ||
      message.agentSessionId !== scope.agentSessionId
    ) {
      throw new Error(
        "canonical Agent activity message is outside the optimistic overlay scope"
      );
    }
    return cloneMessage({
      ...message,
      workspaceId: scope.workspaceId
    });
  });
}

function deleteScopeEntries<T>(entries: Map<string, T>, prefix: string): void {
  for (const key of entries.keys()) {
    if (key.startsWith(prefix)) {
      entries.delete(key);
    }
  }
}

function cloneMessage(message: AgentActivityMessage): AgentActivityMessage {
  return cloneAgentActivityMessage(message);
}
