import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import { useCallback } from "react";
import {
  useAgentSessionPagingState,
  type AgentSessionViewRef
} from "./useAgentSessionPagingState";

export function useAgentSessionControllerState(
  activeRef: AgentSessionViewRef,
  oldestLoadedVersion: number | null = null,
  hasOlderMessages = false
) {
  const paging = useAgentSessionPagingState();
  const getAgentSessionView = useCallback(
    (ref: AgentSessionViewRef) => {
      const pagingEntry = paging.get(ref);
      return pagingEntry
        ? {
            ...pagingEntry,
            hasOlderMessages: false,
            oldestLoadedVersion: null
          }
        : null;
    },
    [paging.get]
  );
  const deleteAgentSessionView = useCallback(
    (ref: AgentSessionViewRef) => paging.remove(ref),
    [paging.remove]
  );
  const setAgentSessionViewMessagesLoading = useCallback(
    (ref: AgentSessionViewRef, value: boolean) =>
      paging.flag("isLoadingMessages", ref, value),
    [paging.flag]
  );
  const setAgentSessionViewOlderMessagesLoading = useCallback(
    (ref: AgentSessionViewRef, value: boolean) =>
      paging.flag("isLoadingOlderMessages", ref, value),
    [paging.flag]
  );
  void paging.entries;
  const storedActiveView = getAgentSessionView(activeRef);
  const activeSessionView =
    storedActiveView === null && oldestLoadedVersion === null
      ? null
      : {
          ...(storedActiveView ?? {
            error: null,
            isLoadingMessages: false,
            isLoadingOlderMessages: false
          }),
          hasOlderMessages,
          oldestLoadedVersion
        };
  return {
    activeSessionView,
    deleteAgentSessionView,
    getAgentSessionView,
    setAgentSessionViewError: paging.setError,
    setAgentSessionViewMessagesLoading,
    setAgentSessionViewOlderMessagesLoading
  };
}

export function oldestVersion(messages: readonly AgentActivityMessage[]) {
  let oldest: number | null = null;
  for (const { version } of messages) {
    if (Number.isFinite(version) && (oldest === null || version < oldest))
      oldest = version;
  }
  return oldest;
}
