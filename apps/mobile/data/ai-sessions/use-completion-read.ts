import { useCallback, useEffect, useRef, useState } from "react";
import { AppState, type ViewToken } from "react-native";
import type { LocalV2Snapshot } from "@multica/core/local-conversation/v2";
import type { AISessionClient } from "./client";
import type { ConversationRow } from "./projection";
import { shownCompletion } from "./completion-read";

interface CompletionReadOptions {
  client: Pick<AISessionClient, "completionReadSupported" | "storeGeneration" | "readCompletion"> | null;
  session: string;
  snapshot: LocalV2Snapshot | undefined;
  rows: readonly ConversationRow[];
  enabled: boolean;
}

/** Route visibility and native foreground state must both permit a read. */
export function useCompletionRead({ client, session, snapshot, rows, enabled }: CompletionReadOptions) {
  const [visibleRows, setVisibleRows] = useState<string[]>([]);
  const acknowledged = useRef(new Set<string>());
  const onViewableItemsChanged = useCallback(({ viewableItems }: { viewableItems: ViewToken<ConversationRow>[] }) => {
    const ids = [...new Set(viewableItems.flatMap(({ item, isViewable }) => isViewable && item.role === "assistant" ? [item.id] : []))].sort();
    setVisibleRows((previous) => previous.length === ids.length && previous.every((id, index) => id === ids[index]) ? previous : ids);
  }, []);

  useEffect(() => {
    if (!enabled || !client?.completionReadSupported) return;
    const check = () => {
      const position = shownCompletion(snapshot, rows, visibleRows, AppState.currentState === "active");
      if (!position || position.store_generation !== client.storeGeneration) return;
      const key = JSON.stringify([session, position]);
      if (acknowledged.current.has(key)) return;
      acknowledged.current.add(key);
      void client.readCompletion(session, position.store_generation, position.turn_id, position.turn_index, position.completion_seq)
        .catch(() => { acknowledged.current.delete(key); });
    };
    // Recheck on foreground recovery even if no snapshot or viewport changed.
    const subscription = AppState.addEventListener("change", check);
    check();
    return () => subscription.remove();
  }, [client, enabled, rows, session, snapshot, visibleRows]);

  return onViewableItemsChanged;
}
