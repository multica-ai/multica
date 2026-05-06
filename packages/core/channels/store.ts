import { create } from "zustand";
import type { StorageAdapter } from "../types";
import { getCurrentSlug, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { createLogger } from "../logger";

// Channels store. Phase 1 only persists per-channel input drafts so users
// don't lose typing when switching channels. The "active channel" is
// derived from the URL slug (`/[ws]/channels/[id]`), not stored here.

const logger = createLogger("channels.store");

const DRAFTS_KEY = "multica:channels:drafts";

export interface ChannelsState {
  /** Per-channel draft text keyed by channel id. */
  inputDrafts: Record<string, string>;
  setInputDraft: (channelId: string, draft: string) => void;
  clearInputDraft: (channelId: string) => void;
}

export interface ChannelsStoreOptions {
  storage: StorageAdapter;
}

function readDrafts(storage: StorageAdapter, key: string): Record<string, string> {
  const raw = storage.getItem(key);
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null
      ? (parsed as Record<string, string>)
      : {};
  } catch {
    return {};
  }
}

function writeDrafts(storage: StorageAdapter, key: string, drafts: Record<string, string>) {
  const pruned: Record<string, string> = {};
  for (const [k, v] of Object.entries(drafts)) {
    if (v) pruned[k] = v;
  }
  if (Object.keys(pruned).length === 0) {
    storage.removeItem(key);
  } else {
    storage.setItem(key, JSON.stringify(pruned));
  }
}

export function createChannelsStore(options: ChannelsStoreOptions) {
  const { storage } = options;
  const wsKey = (base: string) => {
    const slug = getCurrentSlug();
    return slug ? `${base}:${slug}` : base;
  };

  const store = create<ChannelsState>((set, get) => ({
    inputDrafts: readDrafts(storage, wsKey(DRAFTS_KEY)),

    setInputDraft: (channelId, draft) => {
      const next = { ...get().inputDrafts, [channelId]: draft };
      writeDrafts(storage, wsKey(DRAFTS_KEY), next);
      set({ inputDrafts: next });
    },

    clearInputDraft: (channelId) => {
      const cur = get().inputDrafts;
      if (!(channelId in cur)) return;
      const next = { ...cur };
      delete next[channelId];
      writeDrafts(storage, wsKey(DRAFTS_KEY), next);
      set({ inputDrafts: next });
    },
  }));

  registerForWorkspaceRehydration(() => {
    const next = readDrafts(storage, wsKey(DRAFTS_KEY));
    logger.info("workspace rehydration", { draftCount: Object.keys(next).length });
    store.setState({ inputDrafts: next });
  });

  return store;
}
