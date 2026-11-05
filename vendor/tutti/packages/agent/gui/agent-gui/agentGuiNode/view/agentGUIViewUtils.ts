import type { WorkspaceFileReferenceCopy } from "@tutti-os/workspace-file-reference/contracts";
import { useCallback, useLayoutEffect, useMemo, useRef } from "react";
import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import type { AgentGUIViewLabels } from "../AgentGUINodeView";
import type { AgentGUIRailViewModel } from "../model/agentGuiNodeTypes";
import { resolveAgentGUIConversationDisplayTitle } from "../model/agentGuiProviderIdentity";
export function objectRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

export function numberValue(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

export function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

export function useStableEventCallback<Args extends unknown[], Result>(
  callback: (...args: Args) => Result
): (...args: Args) => Result {
  const callbackRef = useRef(callback);
  useLayoutEffect(() => {
    callbackRef.current = callback;
  }, [callback]);
  return useCallback((...args: Args) => callbackRef.current(...args), []);
}

export function useOptionalStableEventCallback<Args extends unknown[], Result>(
  callback: ((...args: Args) => Result) | null | undefined
): ((...args: Args) => Result) | undefined {
  const callbackRef = useRef(callback);
  useLayoutEffect(() => {
    callbackRef.current = callback;
  }, [callback]);
  return useMemo(() => {
    if (callback == null) {
      return undefined;
    }
    return (...args: Args) => callbackRef.current?.(...args) as Result;
  }, [callback != null]);
}

export const fallbackWorkspaceFileReferenceCopy: WorkspaceFileReferenceCopy = {
  t(key, values) {
    return values ? `${key}:${JSON.stringify(values)}` : key;
  }
};

export function agentGuiPerfNowMs(): number {
  return globalThis.performance?.now?.() ?? Date.now();
}

export function roundAgentGuiPerfMs(value: number): number {
  return Math.round(value * 100) / 100;
}

export function conversationPlainTitle(
  conversation: Pick<
    AgentGUIRailViewModel["conversations"][number],
    "title" | "titleFallback"
  >,
  labels: Pick<AgentGUIViewLabels, "untitledConversationTitle">,
  uiLanguage: UiLanguage
): string {
  void uiLanguage;
  return resolveAgentGUIConversationDisplayTitle(
    conversation,
    labels.untitledConversationTitle
  );
}
