export { CoreProvider } from "./core-provider";
export type { CoreProviderProps, ClientIdentity } from "./types";
export { AuthInitializer } from "./auth-initializer";
export { defaultStorage } from "./storage";
export { createPersistStorage } from "./persist-storage";
export { createWorkspaceAwareStorage, setCurrentWorkspace, getCurrentSlug, getCurrentWsId, subscribeToCurrentSlug, registerForWorkspaceRehydration } from "./workspace-storage";
export { clearWorkspaceStorage } from "./storage-cleanup";
export {
  VIBES_PUSH_SERVICE_WORKER_URL,
  ensureWebPushSubscription,
  getWebPushCapability,
  revokeWebPushSubscription,
  type WebPushCapability,
  type WebPushSubscriptionJSON,
} from "./web-push";
