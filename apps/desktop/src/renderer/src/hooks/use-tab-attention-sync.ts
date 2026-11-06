import { useCallback } from "react";
import { useWSEvent } from "@multica/core/realtime";
import type { TaskCompletedPayload, TaskFailedPayload } from "@multica/core/types";
import { useTabStore } from "@/stores/tab-store";
import { attentionTargetTabIds, useTabAttentionStore } from "@/stores/tab-attention-store";

/**
 * Wires finished runs to tab attention dots.
 *
 * The WebSocket is bound to the active workspace, which is exactly the set of
 * tabs the strip shows, so workspace filtering is already done by the time a
 * payload arrives. `task:completed` / `task:failed` are the run-end signals
 * for both surfaces: chat runs carry `chat_session_id`, issue runs do not.
 * Cancellation is excluded — it is a deliberate act, not a result to read.
 *
 * Clearing is not done here: the tab item clears its own dot when it becomes
 * the active (visible) tab, which keeps this hook off the tab store's
 * subscription surface (tests mock the store as a callable with `getState`).
 */
export function useTabAttentionSync() {
  const markTabAttention = useTabAttentionStore((s) => s.markTabAttention);

  const markFromTaskEvent = useCallback(
    (payload: TaskCompletedPayload | TaskFailedPayload) => {
      const { byWorkspace, activeWorkspaceSlug } = useTabStore.getState();
      for (const tabId of attentionTargetTabIds(byWorkspace, activeWorkspaceSlug, payload)) {
        markTabAttention(tabId);
      }
    },
    [markTabAttention],
  );

  useWSEvent("task:completed", (p) => markFromTaskEvent(p as TaskCompletedPayload));
  useWSEvent("task:failed", (p) => markFromTaskEvent(p as TaskFailedPayload));
}
