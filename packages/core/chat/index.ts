export { createChatStore } from "./store";
export type { ChatStoreOptions, ChatState, ChatTimelineItem } from "./store";

import type { createChatStore as CreateChatStoreFn } from "./store";

type ChatStoreInstance = ReturnType<typeof CreateChatStoreFn>;

/** Module-level singleton — set once at app boot via `registerChatStore()`. */
let _store: ChatStoreInstance | null = null;

/**
 * Register the chat store instance created by the app.
 * Must be called at boot before any component renders.
 */
export function registerChatStore(store: ChatStoreInstance) {
  _store = store;
}

/**
 * Singleton accessor — a Zustand hook backed by the registered instance.
 * Supports `useChatStore(selector)` and `useChatStore.getState()`.
 */
export const useChatStore: ChatStoreInstance = new Proxy(
  (() => {}) as unknown as ChatStoreInstance,
  {
    apply(_target, _thisArg, args) {
      if (!_store)
        throw new Error(
          "Chat store not initialised — call registerChatStore() first",
        );
      return (_store as unknown as (...a: unknown[]) => unknown)(...args);
    },
    get(_target, prop) {
      if (!_store) return undefined;
      return Reflect.get(_store, prop);
    },
  },
);
