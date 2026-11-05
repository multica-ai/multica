import { type PendingActivationIntentRecord } from "@tutti-os/agent-activity-core";
import { useCallback, useMemo, useRef } from "react";
import { type AppErrorCode } from "../../../shared/contracts/dto";
import type { AgentConversationServices } from "../../../state/conversation-services";

type AgentGUILiveState = "inactive" | "activating" | "active" | "failed";

type AgentGUIActivateInput = Parameters<
  AgentConversationServices["activations"]["request"]
>[0] extends infer TInput
  ? TInput extends { requestId: string }
    ? Omit<TInput, "requestId">
    : never
  : never;

interface UseAgentGUIActivationInput {
  engine: AgentConversationServices;
  workspaceId: string;
  getErrorMessage: (error: unknown) => string;
  getErrorCode?: (error: unknown) => AppErrorCode | null;
}

export function isPendingNewConversationActivation(
  activation:
    | Pick<PendingActivationIntentRecord, "mode" | "status">
    | null
    | undefined
): boolean {
  return (
    activation?.mode === "new" &&
    (activation.status === "requested" || activation.status === "uncertain")
  );
}

export function isPendingNewConversationActivationForSession(
  activation:
    | Pick<PendingActivationIntentRecord, "agentSessionId" | "mode" | "status">
    | null
    | undefined,
  agentSessionId: string | null | undefined
): boolean {
  const normalizedSessionId = agentSessionId?.trim() ?? "";
  return Boolean(
    normalizedSessionId &&
      activation?.agentSessionId.trim() === normalizedSessionId &&
      isPendingNewConversationActivation(activation)
  );
}

export function useAgentGUIActivation({
  engine,
  workspaceId,
  getErrorMessage,
  getErrorCode
}: UseAgentGUIActivationInput) {
  const requestSequenceRef = useRef(0);
  const nextRequestId = (kind: string, agentSessionId: string): string => {
    requestSequenceRef.current += 1;
    return `${kind}:${workspaceId}:${agentSessionId}:${Date.now()}:${requestSequenceRef.current}`;
  };

  const activate = useCallback(
    (input: AgentGUIActivateInput): string | null => {
      const agentSessionId = input.agentSessionId.trim();
      const requestId = nextRequestId("activation", agentSessionId);
      return engine.activations.request({
        ...input,
        agentSessionId,
        requestId
      })
        ? requestId
        : null;
    },
    [engine, workspaceId]
  );

  const unactivate = useCallback(
    (agentSessionId: string): Promise<void> => {
      const normalized = agentSessionId.trim();
      if (!normalized) {
        return Promise.resolve();
      }
      engine.activations.unactivate(normalized);
      return Promise.resolve();
    },
    [engine, workspaceId]
  );

  const markFailed = useCallback(
    (agentSessionId: string, error: unknown): void => {
      const normalized = agentSessionId.trim();
      if (!normalized) {
        return;
      }
      engine.activations.recordFailure(
        {
          mode: "existing",
          agentSessionId: normalized,
          requestId: nextRequestId("activation-failure", normalized)
        },
        { code: getErrorCode?.(error) ?? null, message: getErrorMessage(error) }
      );
    },
    [engine, getErrorCode, getErrorMessage, workspaceId]
  );

  const clearFailure = useCallback(
    (agentSessionId: string): void => {
      const normalized = agentSessionId.trim();
      if (normalized) {
        engine.activations.clearFailure(normalized);
      }
    },
    [engine]
  );

  const stateFor = useCallback(
    (agentSessionId: string | null | undefined): AgentGUILiveState =>
      (agentSessionId
        ? engine.activations.presentation(agentSessionId).status
        : null) ?? "inactive",
    [engine]
  );
  const errorFor = useCallback(
    (agentSessionId: string | null | undefined): string | null =>
      (agentSessionId
        ? engine.activations.presentation(agentSessionId).errorMessage
        : null) ?? null,
    [engine]
  );
  const codeFor = useCallback(
    (agentSessionId: string | null | undefined): AppErrorCode | null =>
      ((agentSessionId
        ? engine.activations.presentation(agentSessionId).errorCode
        : null) as AppErrorCode | null) ?? null,
    [engine]
  );

  return useMemo(
    () => ({
      activate,
      clearFailure,
      markFailed,
      unactivate,
      stateFor,
      errorFor,
      codeFor
    }),
    [
      activate,
      clearFailure,
      codeFor,
      errorFor,
      markFailed,
      stateFor,
      unactivate
    ]
  );
}
