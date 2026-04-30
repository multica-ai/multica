// Channels module — query options, mutation hooks, and the per-workspace
// drafts store. The store is a Proxy-backed singleton: the platform layer
// constructs the real instance at boot and registers it via
// registerChannelsStore. Components use useChannelsStore(...selector...) and
// don't see the platform-specific constructor.

export { createChannelsStore } from "./store";
export type { ChannelsState, ChannelsStoreOptions } from "./store";
export {
  channelKeys,
  channelsListOptions,
  channelDetailOptions,
  channelMembersOptions,
  channelMessagesOptions,
  channelMessageThreadOptions,
} from "./queries";
export {
  useCreateChannel,
  useUpdateChannel,
  useArchiveChannel,
  useAddChannelMember,
  useRemoveChannelMember,
  useSendChannelMessage,
  useMarkChannelRead,
  useCreateOrFetchDM,
  useToggleChannelReaction,
} from "./mutations";

import type { createChannelsStore as CreateChannelsStoreFn } from "./store";

type ChannelsStoreInstance = ReturnType<typeof CreateChannelsStoreFn>;

let _store: ChannelsStoreInstance | null = null;

/**
 * Register the channels store instance created by the app.
 * Must be called at boot before any channels component renders.
 */
export function registerChannelsStore(store: ChannelsStoreInstance) {
  _store = store;
}

/**
 * Singleton accessor — a Zustand hook backed by the registered instance.
 * Supports `useChannelsStore(selector)` and `useChannelsStore.getState()`.
 */
export const useChannelsStore: ChannelsStoreInstance = new Proxy(
  (() => {}) as unknown as ChannelsStoreInstance,
  {
    apply(_target, _thisArg, args) {
      if (!_store)
        throw new Error(
          "Channels store not initialised — call registerChannelsStore() first",
        );
      return (_store as unknown as (...a: unknown[]) => unknown)(...args);
    },
    get(_target, prop) {
      if (!_store) return undefined;
      return Reflect.get(_store, prop);
    },
  },
);
