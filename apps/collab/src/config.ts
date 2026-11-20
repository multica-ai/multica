export interface CollabConfig {
  port: number;
  /** Shared with the Go API: signs editor tickets and authenticates the
   *  internal calls in both directions. */
  secret: string;
  /** Base URL of the Go API's internal collaboration endpoints. */
  apiUrl: string;
  cdnDomain: string;
  /** Quiet period before a changed document is written back. */
  storeDebounceMs: number;
  /** Longest a continuously edited document goes unsaved. */
  storeMaxDebounceMs: number;
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): CollabConfig {
  const secret = env.COLLAB_SECRET?.trim() ?? "";
  const apiUrl = env.MULTICA_API_URL?.trim().replace(/\/+$/, "") ?? "";
  if (!secret) throw new Error("COLLAB_SECRET is required");
  if (!apiUrl) throw new Error("MULTICA_API_URL is required");
  return {
    port: Number(env.COLLAB_PORT || 8095),
    secret,
    apiUrl,
    cdnDomain: env.MULTICA_CDN_DOMAIN?.trim() ?? "",
    storeDebounceMs: Number(env.COLLAB_STORE_DEBOUNCE_MS || 2000),
    storeMaxDebounceMs: Number(env.COLLAB_STORE_MAX_DEBOUNCE_MS || 10000),
  };
}
