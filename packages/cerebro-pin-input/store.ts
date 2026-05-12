import { create } from "zustand";

/**
 * Tracks which comment-/reply-input is currently "pinned" to the bottom of
 * the viewport. Only one input may be pinned at a time across the whole app;
 * pinning a different input automatically releases the previous one.
 *
 * The state is intentionally not persisted — pinning is an in-the-moment
 * affordance for a long reply, not a setting the user expects to outlive a
 * page reload.
 */
interface PinInputStore {
  pinnedKey: string | null;
  pin: (key: string) => void;
  unpin: (key: string) => void;
  unpinAll: () => void;
}

export const usePinInputStore = create<PinInputStore>((set) => ({
  pinnedKey: null,
  pin: (key) => set({ pinnedKey: key }),
  unpin: (key) =>
    set((s) => (s.pinnedKey === key ? { pinnedKey: null } : s)),
  unpinAll: () => set({ pinnedKey: null }),
}));

export function selectIsPinned(key: string) {
  return (s: PinInputStore) => s.pinnedKey === key;
}
