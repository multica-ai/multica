import { createStore } from "zustand/vanilla";
import {
readSavedPromptQueues,
type SavedPromptQueue
} from "./saved-prompt-queues";

interface DesktopPendingState {
  queues: SavedPromptQueue[];
  durable: boolean;
  setQueues(queues: SavedPromptQueue[]): void;
}

/** Checkpoint only submissions whose ownership has not transferred to Electron. */
export function createDesktopPendingStore(
  storage?: Pick<Storage, "getItem" | "setItem" | "removeItem">,
  namespace?: string
) {
  const key = namespace ? `multica:desktop-pending:v1:${namespace}` : null;
  let queues: SavedPromptQueue[] = [];
  if (storage && key) {
    try {
      queues = readSavedPromptQueues(
        JSON.parse(storage.getItem(key) ?? "null")
      );
    } catch (error) {
      console.warn("Could not restore pending submissions", error);
    }
  }
  return createStore<DesktopPendingState>()((set) => ({
    queues,
    durable: Boolean(storage && key),
    setQueues(next) {
      const queues = readSavedPromptQueues(next);
      let durable = false;
      if (storage && key) {
        try {
          if (queues.length) storage.setItem(key, JSON.stringify(queues));
          else storage.removeItem(key);
          durable = true;
        } catch (error) {
          console.warn("Could not save pending submissions", error);
        }
      }
      set({ queues, durable });
    }
  }));
}

export type DesktopPendingStore = ReturnType<typeof createDesktopPendingStore>;
