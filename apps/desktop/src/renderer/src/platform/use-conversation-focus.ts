import { useEffect } from "react";
import type { LocalV2ScopeClient } from "./local-v2";

/**
 * Reports the conversation on show. Only the active tab's page is mounted, so
 * active plus a visible window means someone can actually be looking at it.
 */
export function useConversationFocus(client: LocalV2ScopeClient, conversationId: string | null, active: boolean): void {
  useEffect(() => {
    if (!conversationId || !active) return;
    let release: (() => void) | undefined;
    const sync = () => {
      if (document.visibilityState === "visible") release ??= client.focus(conversationId);
      else { release?.(); release = undefined; }
    };
    sync();
    document.addEventListener("visibilitychange", sync);
    return () => {
      document.removeEventListener("visibilitychange", sync);
      release?.();
    };
  }, [client, conversationId, active]);
}
