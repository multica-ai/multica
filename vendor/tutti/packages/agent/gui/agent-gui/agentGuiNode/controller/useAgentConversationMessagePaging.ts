import { useCallback, useEffect, useMemo, useRef } from "react";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import { isWorkspaceAgentActivityOptimisticMessage } from "../../../shared/workspaceAgentMessageOverlay";
import {
  createAgentConversationMessageController,
  type AgentConversationMessageController
} from "../../../agentConversationMessageController";

const PAGE_SIZE = 100;

export function minFiniteMessageVersion(
  messages: readonly AgentActivityMessage[]
): number | null {
  let result: number | null = null;
  for (const message of messages) {
    if (
      !Number.isFinite(message.version) ||
      isWorkspaceAgentActivityOptimisticMessage(message)
    )
      continue;
    result =
      result === null ? message.version : Math.min(result, message.version);
  }
  return result;
}

export function maxFiniteMessageVersion(
  messages: readonly AgentActivityMessage[]
): number | null {
  let result: number | null = null;
  for (const message of messages) {
    if (
      !Number.isFinite(message.version) ||
      isWorkspaceAgentActivityOptimisticMessage(message)
    )
      continue;
    result =
      result === null ? message.version : Math.max(result, message.version);
  }
  return result;
}

function messageText(message: AgentActivityMessage): string {
  const payload = message.payload;
  for (const key of ["displayPrompt", "text"] as const) {
    const value = payload[key];
    if (typeof value === "string" && value.trim()) return value;
  }
  const content = payload.content;
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .map((block) => {
      if (!block || typeof block !== "object" || Array.isArray(block))
        return "";
      const text = (block as { text?: unknown }).text;
      return typeof text === "string" ? text : "";
    })
    .filter(Boolean)
    .join("\n");
}

function isUserTextMessage(message: AgentActivityMessage): boolean {
  return (
    message.kind.trim().toLowerCase() === "text" &&
    message.role.trim().toLowerCase() === "user" &&
    messageText(message).trim() !== ""
  );
}

export function windowHasTurnMissingUserPrompt(
  messages: readonly AgentActivityMessage[],
  newestPagedVersion: number | null
): boolean {
  if (newestPagedVersion === null) {
    return messages.length > 0 && !messages.some(isUserTextMessage);
  }
  const turnIdsWithUserPrompt = new Set<string>();
  const pagedTurnIds = new Set<string>();
  for (const message of messages) {
    if (isWorkspaceAgentActivityOptimisticMessage(message)) continue;
    const turnId = message.turnId?.trim() ?? "";
    if (!turnId) continue;
    if (
      Number.isFinite(message.version) &&
      message.version <= newestPagedVersion
    ) {
      pagedTurnIds.add(turnId);
    }
    if (isUserTextMessage(message)) turnIdsWithUserPrompt.add(turnId);
  }
  return [...pagedTurnIds].some((turnId) => !turnIdsWithUserPrompt.has(turnId));
}

export function filterMessagesForOptimisticDetailWindow(input: {
  detailMessages: readonly AgentActivityMessage[];
  localMessages: readonly AgentActivityMessage[];
}): AgentActivityMessage[] {
  const optimisticTurnIds = new Set(
    input.detailMessages
      .filter(isWorkspaceAgentActivityOptimisticMessage)
      .map((message) => message.turnId?.trim() ?? "")
      .filter(Boolean)
  );
  if (optimisticTurnIds.size === 0) return [];
  return input.localMessages.filter((message) => {
    if (isWorkspaceAgentActivityOptimisticMessage(message)) return true;
    const turnId = message.turnId?.trim() ?? "";
    return turnId !== "" && optimisticTurnIds.has(turnId);
  });
}

export function filterMessagesForDetailWindowOverlay(input: {
  detailMessages: readonly AgentActivityMessage[];
  durableMessages: readonly AgentActivityMessage[];
  localMessages: readonly AgentActivityMessage[];
}): AgentActivityMessage[] {
  if (input.localMessages.length === 0) return [];
  if (input.detailMessages.length === 0) {
    if (input.durableMessages.length <= PAGE_SIZE)
      return [...input.localMessages];
    const newest = maxFiniteMessageVersion(input.durableMessages);
    return input.localMessages.filter(
      (message) =>
        isWorkspaceAgentActivityOptimisticMessage(message) ||
        (newest !== null &&
          Number.isFinite(message.version) &&
          message.version >= newest)
    );
  }
  const bounded = input.detailMessages.filter(
    (message) => !isWorkspaceAgentActivityOptimisticMessage(message)
  );
  const oldest = minFiniteMessageVersion(bounded);
  const newest = maxFiniteMessageVersion(bounded);
  if (oldest === null && newest === null) {
    const optimistic = filterMessagesForOptimisticDetailWindow(input);
    return optimistic.length > 0 ||
      input.detailMessages.some(isWorkspaceAgentActivityOptimisticMessage)
      ? optimistic
      : [...input.localMessages];
  }
  return input.localMessages.filter(
    (message) =>
      isWorkspaceAgentActivityOptimisticMessage(message) ||
      !Number.isFinite(message.version) ||
      (newest !== null && message.version > newest) ||
      (oldest !== null && message.version >= oldest)
  );
}

export function sessionHasRenderableMessages(input: {
  agentSessionId: string;
  snapshotMessagesById: Record<string, AgentActivityMessage[]>;
}): boolean {
  const normalized = input.agentSessionId.trim();
  if (!normalized) return false;
  return (input.snapshotMessagesById[normalized]?.length ?? 0) > 0;
}

export interface ConversationMessagePagingDiagnosticsPort {
  error(input: {
    agentSessionId: string;
    context?: Record<string, unknown>;
    error: unknown;
    phase: "load_session_messages" | "synchronize_session";
  }): void;
  page(input: {
    agentSessionId: string;
    details: Record<string, unknown>;
    event: string;
    level?: "debug" | "warn";
    messages?: readonly AgentActivityMessage[];
  }): void;
}

export interface AgentConversationMessagePagingInput {
  diagnostics: ConversationMessagePagingDiagnosticsPort;
  getActiveSessionId(): string | null;
  isMounted(): boolean;
  onOlderPageLoadingChanged(loading: boolean): void;
  runtime: AgentGUIRuntime;
  sessionEngine: import("@tutti-os/agent-activity-core").AgentSessionEngine;
  workspaceId: string;
}

export function useAgentConversationMessagePaging(
  input: AgentConversationMessagePagingInput
) {
  const inputRef = useRef(input);
  inputRef.current = input;
  const controller = useMemo(
    () =>
      createAgentConversationMessageController({
        diagnostics: {
          error: ({ agentSessionId, beforeVersion, error }) =>
            inputRef.current.diagnostics.error({
              agentSessionId,
              context: { beforeVersion },
              error,
              phase: "load_session_messages"
            }),
          page: (event) => inputRef.current.diagnostics.page(event),
          synchronizationError: ({ agentSessionId, error }) =>
            inputRef.current.diagnostics.error({
              agentSessionId,
              error,
              phase: "synchronize_session"
            })
        },
        engine: input.sessionEngine,
        ensureSessionSynchronized: ({ agentSessionId, onError, workspaceId }) =>
          inputRef.current.runtime.ensureSessionSynchronized?.({
            agentSessionId,
            onError,
            workspaceId
          }) ?? (() => {}),
        isAvailable: (agentSessionId) =>
          inputRef.current.isMounted() &&
          (!agentSessionId ||
            inputRef.current.getActiveSessionId()?.trim() === agentSessionId),
        ...(inputRef.current.runtime.loadOlderSessionMessages
          ? { loadOlderSessionMessages: (older: { workspaceId: string; agentSessionId: string }) => inputRef.current.runtime.loadOlderSessionMessages!(older) }
          : {}),
        listSessionMessages: ({
          agentSessionId,
          beforeVersion,
          limit,
          order,
          signal,
          workspaceId
        }) =>
          inputRef.current.runtime.listSessionMessages({
            agentSessionId,
            beforeVersion,
            cache: false,
            limit,
            order,
            signal,
            workspaceId
          }),
        onSnapshotChanged: (snapshot) =>
          inputRef.current.onOlderPageLoadingChanged(
            snapshot.olderPagePhase === "loading"
          ),
        workspaceId: input.workspaceId
      }),
    [input.sessionEngine, input.workspaceId]
  );
  const controllerLifetimeRef = useRef<{
    controller: AgentConversationMessageController;
    generation: number;
  } | null>(null);
  if (controllerLifetimeRef.current?.controller !== controller) {
    controllerLifetimeRef.current = { controller, generation: 0 };
  }
  const controllerLifetime = controllerLifetimeRef.current;
  useEffect(() => {
    const generation = ++controllerLifetime.generation;
    return () => {
      // React development Strict Mode and Fast Refresh may immediately replay
      // an effect cleanup/setup pair against the same memoized controller.
      // Delay permanent disposal for one microtask so a replay can renew the
      // ownership generation; real unmounts and controller replacements still
      // dispose the abandoned instance.
      queueMicrotask(() => {
        if (controllerLifetime.generation === generation) {
          controller.dispose();
        }
      });
    };
  }, [controller, controllerLifetime]);

  const loadInitialMessages = useCallback(
    async (agentSessionId: string, options?: { force?: boolean }) => {
      const normalized = agentSessionId.trim();
      if (!normalized) return;
      controller.requestInitial(normalized, options);
    },
    [controller]
  );

  const loadOlderMessages = useCallback(
    async (agentSessionId?: string | null) => {
      const current = inputRef.current;
      controller.setActiveSession(current.getActiveSessionId());
      await controller.loadOlder(agentSessionId);
    },
    [controller]
  );

  return {
    loadInitialMessages,
    loadOlderMessages,
    setActiveSession: controller.setActiveSession
  };
}
