import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import {
  LIVE_MODELS_STALE_TIME_MS,
  resolveRuntimeModels,
  runtimeModelsKeys,
  runtimeModelsOptions,
  staleTimeFor,
} from "./models";
import type { RuntimeModelListRequest, RuntimeModelsResult } from "../types/agent";

const initiateListModels = vi.fn();
const getListModelsResult = vi.fn();

vi.mock("../api", () => ({
  api: {
    initiateListModels: (runtimeId: string) => initiateListModels(runtimeId),
    getListModelsResult: (runtimeId: string, requestId: string) =>
      getListModelsResult(runtimeId, requestId),
  },
}));

const catalog = [{ id: "claude-sonnet-4-6", label: "Claude Sonnet 4.6" }];
const refreshedCatalog = [{ id: "claude-opus-5", label: "Claude Opus 5" }];

function request(
  overrides: Partial<RuntimeModelListRequest>,
): RuntimeModelListRequest {
  return {
    id: "req-1",
    runtime_id: "rt-1",
    status: "pending",
    supported: true,
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T00:00:00Z",
    ...overrides,
  };
}

function cachedResponse(
  models: RuntimeModelListRequest["models"],
  cachedAt: string,
): RuntimeModelListRequest {
  return request({
    status: "completed",
    models,
    cached: true,
    cached_at: cachedAt,
  });
}

beforeEach(() => {
  initiateListModels.mockReset();
  getListModelsResult.mockReset();
});

describe("resolveRuntimeModels", () => {
  // The server answers a warm runtime straight from its catalog cache
  // (MUL-5444). That response is already terminal, so discovery must resolve on
  // the POST alone — one round trip, no polling, no spinner.
  it("resolves from a cached completed response without polling", async () => {
    initiateListModels.mockResolvedValue(
      cachedResponse(catalog, "2026-07-29T00:00:00Z"),
    );

    const result = await resolveRuntimeModels("rt-1");

    expect(result).toEqual({
      models: catalog,
      supported: true,
      cached: true,
      cachedAt: "2026-07-29T00:00:00Z",
    });
    expect(getListModelsResult).not.toHaveBeenCalled();
  });

  it("marks a live discovery as not cached", async () => {
    initiateListModels.mockResolvedValue(
      request({ status: "completed", models: catalog }),
    );

    const result = await resolveRuntimeModels("rt-1");

    expect(result.cached).toBe(false);
    expect(result.cachedAt).toBeUndefined();
  });

  it("still polls a pending response until the daemon reports back", async () => {
    initiateListModels.mockResolvedValue(request({ status: "pending" }));
    getListModelsResult
      .mockResolvedValueOnce(request({ status: "running" }))
      .mockResolvedValueOnce(request({ status: "completed", models: catalog }));

    const result = await resolveRuntimeModels("rt-1");

    expect(result.models).toEqual(catalog);
    expect(result.supported).toBe(true);
    expect(getListModelsResult).toHaveBeenCalledTimes(2);
    expect(getListModelsResult).toHaveBeenLastCalledWith("rt-1", "req-1");
  });

  it("surfaces a failed discovery as an error", async () => {
    initiateListModels.mockResolvedValue(
      request({ status: "failed", error: "claude not installed" }),
    );

    await expect(resolveRuntimeModels("rt-1")).rejects.toThrow(
      "claude not installed",
    );
  });

  // A status this client does not know (newer server, or the malformed-response
  // fallback record) must NOT read as "completed with no models" — that renders
  // an authoritative-looking empty dropdown. The schema keeps `status` lenient,
  // so such a value really can reach this code at runtime even though the TS
  // union does not admit it.
  it("treats an unrecognised status as an explicit failure", async () => {
    initiateListModels.mockResolvedValue({
      ...request({}),
      status: "superseded" as RuntimeModelListRequest["status"],
    });

    await expect(resolveRuntimeModels("rt-1")).rejects.toThrow(
      /status: superseded/,
    );
  });

  it("defaults supported to true when the server omits it", async () => {
    initiateListModels.mockResolvedValue({
      ...request({ status: "completed", models: catalog }),
      supported: undefined as unknown as boolean,
    });

    const result = await resolveRuntimeModels("rt-1");

    expect(result.supported).toBe(true);
  });
});

describe("staleTimeFor", () => {
  // The server may serve a snapshot up to its own serve window old and refresh
  // it in the background. If the client also held that response as fresh, the
  // observable staleness would be server window + client window. Zero keeps the
  // bound at the server's window alone.
  it("treats a cached answer as immediately revalidatable", () => {
    expect(staleTimeFor({ models: catalog, supported: true, cached: true })).toBe(0);
  });

  it("trusts a live answer for the full window", () => {
    expect(staleTimeFor({ models: catalog, supported: true, cached: false })).toBe(
      LIVE_MODELS_STALE_TIME_MS,
    );
    expect(staleTimeFor({ models: catalog, supported: true })).toBe(
      LIVE_MODELS_STALE_TIME_MS,
    );
  });

  it("has nothing to trust without data", () => {
    expect(staleTimeFor(undefined)).toBe(0);
  });
});

describe("runtimeModelsOptions", () => {
  it("keeps unused entries long enough to render instantly on return", () => {
    const options = runtimeModelsOptions("rt-1");
    expect(options.gcTime).toBeGreaterThanOrEqual(LIVE_MODELS_STALE_TIME_MS);
    expect(options.queryKey).toEqual(["runtimes", "models", "rt-1"]);
    expect(options.enabled).toBe(true);
  });

  it("resolves freshness from the served answer, not a fixed window", () => {
    const options = runtimeModelsOptions("rt-1");
    expect(typeof options.staleTime).toBe("function");
    const staleTime = options.staleTime as (query: {
      state: { data?: { models: []; supported: boolean; cached?: boolean } };
    }) => number;
    expect(
      staleTime({ state: { data: { models: [], supported: true, cached: true } } }),
    ).toBe(0);
    expect(
      staleTime({ state: { data: { models: [], supported: true, cached: false } } }),
    ).toBe(LIVE_MODELS_STALE_TIME_MS);
  });

  it("stays disabled without a runtime", () => {
    expect(runtimeModelsOptions(null).enabled).toBe(false);
  });

  // The regression Sol-Boy flagged on PR #6098: a stale-but-served snapshot must
  // reach the SAME client once the server-side refresh lands, and the picker
  // must not blink an empty loading state while that happens.
  it("picks up the refreshed catalog on revisit without a blank loading state", async () => {
    initiateListModels
      // First open: server hands back a 14-minute-old snapshot and queues its
      // own background refresh.
      .mockResolvedValueOnce(cachedResponse(catalog, "2026-07-29T00:00:00Z"))
      // Revisit: the refresh has landed, so the same endpoint now serves the
      // new catalog.
      .mockResolvedValueOnce(
        cachedResponse(refreshedCatalog, "2026-07-29T00:14:00Z"),
      );

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const options = runtimeModelsOptions("rt-1");

    await client.fetchQuery(options);
    expect(
      client.getQueryData(runtimeModelsKeys.forRuntime("rt-1")),
    ).toMatchObject({ models: catalog, cached: true });

    // A cached answer is stale on arrival, so remounting the picker revalidates.
    const query = client
      .getQueryCache()
      .find<RuntimeModelsResult>({
        queryKey: runtimeModelsKeys.forRuntime("rt-1"),
      })!;
    expect(query.isStaleByTime(staleTimeFor(query.state.data))).toBe(true);

    const observer = new QueryObserver(client, options);
    const emissions: { isLoading: boolean; ids: string[] }[] = [];
    const unsubscribe = observer.subscribe((result) => {
      emissions.push({
        isLoading: result.isLoading,
        ids: (result.data?.models ?? []).map((m) => m.id),
      });
    });

    try {
      await vi.waitFor(() => {
        const last = emissions.at(-1);
        expect(last?.ids).toEqual(refreshedCatalog.map((m) => m.id));
      });
    } finally {
      unsubscribe();
      client.clear();
    }

    // Every emission during the background revalidation kept a rendered
    // catalog: the pickers gate their spinner on `isLoading`, so this is what
    // "no blank loading state" means concretely.
    expect(emissions.every((e) => e.isLoading === false)).toBe(true);
    expect(emissions.every((e) => e.ids.length > 0)).toBe(true);
    // The first emission is the stale snapshot still on screen while the
    // revalidation runs — proof the new catalog arrived by replacing rendered
    // data, not after a gap.
    expect(emissions[0]?.ids).toEqual(catalog.map((m) => m.id));
    expect(initiateListModels).toHaveBeenCalledTimes(2);
  });
});
