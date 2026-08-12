import AsyncStorage from "@react-native-async-storage/async-storage";
import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { draftStorageKey, readDraftPartition } from "./draft-persistence";
import type { ChatOutboxItem } from "./chat-outbox";

let skipNextOutboxWrite = false;

const outboxStorage = {
  getItem: (name: string) => AsyncStorage.getItem(name),
  setItem: (name: string, value: string) => {
    if (skipNextOutboxWrite) return Promise.resolve();
    return AsyncStorage.setItem(name, value);
  },
  removeItem: (name: string) => AsyncStorage.removeItem(name),
};

interface ChatOutboxState {
  items: ChatOutboxItem[];
  enqueue: (item: ChatOutboxItem) => void;
  update: (clientId: string, update: (item: ChatOutboxItem) => ChatOutboxItem) => void;
  remove: (clientId: string) => void;
}

export const useChatOutboxStore = create<ChatOutboxState>()(
  persist(
    (set) => ({
      items: [],
      enqueue: (item) => set((state) => ({ items: [...state.items, item] })),
      update: (clientId, update) =>
        set((state) => ({
          items: state.items.map((item) =>
            item.clientId === clientId ? update(item) : item,
          ),
        })),
      remove: (clientId) =>
        set((state) => ({
          items: state.items.filter((item) => item.clientId !== clientId),
        })),
    }),
    {
      name: "multica_draft:outbox:unscoped",
      storage: createJSONStorage(() => outboxStorage),
      skipHydration: true,
      partialize: (state) => ({ items: state.items }),
    },
  ),
);

/** Hydrate one account/workspace partition before its chat screen renders. */
export async function hydrateChatOutbox(userId: string, workspaceSlug: string) {
  const name = draftStorageKey("outbox", userId, workspaceSlug);
  const persisted = await readDraftPartition<Pick<ChatOutboxState, "items">>(
    "outbox",
    userId,
    workspaceSlug,
  );
  useChatOutboxStore.persist.setOptions({
    name,
  });
  useChatOutboxStore.setState({ items: persisted?.items ?? [] });
}

/** Remove the currently hydrated account's in-memory queue on logout or 401. */
export function clearChatOutbox() {
  // `setState` writes through Zustand persist. Suppress this one write because
  // auth cleanup has already removed the active partition; recreating it with
  // an empty value would leave a zombie key behind.
  skipNextOutboxWrite = true;
  try {
    useChatOutboxStore.setState({ items: [] });
  } finally {
    skipNextOutboxWrite = false;
  }
}
