import { useCallback, useEffect, useMemo, useSyncExternalStore } from "react";
import type { V2Conversation } from "@multica/core/local-conversation/conversation";
import type { LocalV2Scope } from "@multica/core/local-conversation/v2";
import { conversationStore } from "./conversation-store";
import { useLocalDevice } from "./local-device";

export interface LiveConversation {
  conversation?: V2Conversation;
  /** Changes whenever the conversation does; V2Conversation updates in place. */
  version: number;
  error: Error | null;
  pending: boolean;
  retry(): void;
}

/** A conversation from the store, held live while enabled. */
export function useLocalV2Conversation(scope: LocalV2Scope, id: string | null | undefined, enabled = true): LiveConversation {
  const device = useLocalDevice();
  const store = conversationStore(device.transport, scope);
  const version = useSyncExternalStore(
    useCallback((listener: () => void) => id ? store.observe(id, listener) : () => {}, [store, id]),
    () => id ? store.version(id) : 0,
  );
  useEffect(() => id && enabled ? store.open(id) : undefined, [store, id, enabled]);
  const conversation = id ? store.conversation(id) : undefined;
  const error = id ? store.status(id).error : null;
  return useMemo(() => ({ conversation, version, error, pending: !!id && !conversation && !error, retry: () => { if (id) store.retry(id); } }),
    [conversation, version, error, id, store]);
}
