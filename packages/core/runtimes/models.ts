import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { RuntimeModelsResult } from "../types/agent";

export const runtimeModelsKeys = {
  all: () => ["runtimes", "models"] as const,
  forRuntime: (runtimeId: string) =>
    [...runtimeModelsKeys.all(), runtimeId] as const,
};

const POLL_INTERVAL_MS = 500;
const POLL_TIMEOUT_MS = 30_000;

// How long a LIVE discovery result (one the daemon just produced) is trusted
// without re-asking. Discovery is a round trip to the user's machine, and a
// catalog only changes when they upgrade a CLI, switch accounts, or edit a
// provider config, so re-running it inside one form session is pure waste.
export const LIVE_MODELS_STALE_TIME_MS = 5 * 60_000;
// Kept long so a runtime revisited later in the same session renders from the
// client cache instead of the "discovering models" spinner. Freshness is
// governed by staleTime; gcTime only decides how long the entry survives while
// unused.
export const MODELS_GC_TIME_MS = 30 * 60_000;

// resolveRuntimeModels initiates a list-models request against the daemon
// (via heartbeat piggyback) and polls until the daemon reports back or
// the request times out. Returns both the models list and a
// `supported` flag: `supported=false` means the provider ignores
// per-agent model selection entirely (hermes today) — the UI uses
// this to disable its dropdown instead of accepting a value that
// wouldn't be honoured at runtime.
//
// `cached` reports that the server answered from its catalog cache rather than
// a live daemon round trip (MUL-5444). It is not cosmetic: it feeds the
// staleTime policy below so the client never extends the server's staleness
// window past what the server itself promises.
export async function resolveRuntimeModels(
  runtimeId: string,
): Promise<RuntimeModelsResult> {
  const initial = await api.initiateListModels(runtimeId);
  const start = Date.now();
  let current = initial;
  while (current.status === "pending" || current.status === "running") {
    if (Date.now() - start > POLL_TIMEOUT_MS) {
      throw new Error("model discovery timed out");
    }
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
    current = await api.getListModelsResult(runtimeId, initial.id);
  }
  // Only an explicit `completed` is a catalog. Anything else — failed, timeout,
  // or a status this client does not know (newer server, or a response that fell
  // back to the malformed-record shape) — is surfaced as an error so the picker
  // shows "discovery failed" and keeps manual entry available. Treating an
  // unrecognised status as success would render an empty dropdown that looks
  // authoritative.
  if (current.status !== "completed") {
    throw new Error(
      current.error || `model discovery failed (status: ${current.status})`,
    );
  }
  return {
    models: current.models ?? [],
    supported: current.supported !== false,
    cached: current.cached === true,
    cachedAt: current.cached_at,
  };
}

// staleTimeFor is the freshness policy, split out so it can be unit-tested
// without a QueryClient.
//
// A cached answer is treated as stale immediately. The server serves a snapshot
// up to `modelCatalogServeWindow` old and queues its own background refresh;
// if the client ALSO held that response as fresh for minutes, the observable
// staleness would be server window + client window (and the refreshed catalog
// would never reach the tab that triggered the refresh). Returning 0 keeps the
// bound at the server's window alone: the next mount / focus revalidates, the
// refreshed snapshot is picked up, and because the query already holds data
// React Query refetches in the background — `isLoading` stays false, so no
// picker flashes an empty loading state (MUL-5444).
export function staleTimeFor(data: RuntimeModelsResult | undefined): number {
  if (!data) return 0;
  return data.cached ? 0 : LIVE_MODELS_STALE_TIME_MS;
}

export function runtimeModelsOptions(runtimeId: string | null | undefined) {
  return queryOptions({
    queryKey: runtimeId
      ? runtimeModelsKeys.forRuntime(runtimeId)
      : runtimeModelsKeys.all(),
    queryFn: () => resolveRuntimeModels(runtimeId as string),
    enabled: Boolean(runtimeId),
    staleTime: (query) => staleTimeFor(query.state.data),
    gcTime: MODELS_GC_TIME_MS,
    retry: false,
  });
}
