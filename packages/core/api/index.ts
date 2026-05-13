export {
  ApiClient,
  ApiError,
  PreviewTooLargeError,
  PreviewUnsupportedError,
} from "./client";
export type {
  ApiClientOptions,
  ImportStarterContentPayload,
  ImportStarterContentResponse,
  ImportStarterIssuePayload,
  ImportStarterWelcomeIssueTemplate,
} from "./client";
export { parseWithFallback, setSchemaLogger } from "./schema";
export type { ParseOptions } from "./schema";
export { WSClient } from "./ws-client";

import type { ApiClient as ApiClientType } from "./client";

/** Module-level singleton — set once at app boot via `setApiInstance()`. */
let _api: ApiClientType | null = null;

export function setApiInstance(instance: ApiClientType) {
  _api = instance;
}

/** Returns the shared ApiClient singleton. Throws if not yet initialised. */
export function getApi(): ApiClientType {
  if (!_api) throw new Error("ApiClient not initialised — call setApiInstance() first");
  return _api;
}

/**
 * Convenience re-export: a proxy that forwards every property access to the
 * singleton so existing call-sites (`api.listIssues(...)`) keep working.
 */
export const api = new Proxy({} as ApiClientType, {
  get(_target, prop, receiver) {
    // Allow property inspection (HMR/React Refresh) before initialisation
    if (!_api) return undefined;
    const value = Reflect.get(_api, prop, receiver);
    return typeof value === "function" ? value.bind(_api) : value;
  },
});
