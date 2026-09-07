import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import {
  LIVE_MODELS_STALE_TIME_MS,
  resolveRuntimeModels,
  refreshRuntimeModels,
  runtimeModelsKeys,
  runtimeModelsOptions,
  staleTimeFor,
} from "./models";
import type { RuntimeModelListRequest, RuntimeModelsResult } from "../types/agent";

const initiateListModels = vi.fn();
const getListModelsResult = vi.fn();

vi.mock("../api", () => ({
  api: {
    initiateListModels: (
      runtimeId: string,
      options?: { force?: boolean },
    ) => initiateListModels(runtimeId, options),
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
      unavailableModels: [],
      supported: true,
      cached: true,
      cachedAt: "2026-07-29T00:00:00Z",
    });
    expect(getListModelsResult).not.toHaveBeenCalled();
  });

  // MUL-6961. This is the single guarantee three separate consumers depend on
  // — the create dropdown, the inspector picker, and the agent builder all read
  // `models` and would each have to be taught otherwise. Models the runtime
  // cannot run stay out of it, so none of them can offer one, and so can no
  // already-installed client that never heard of the second list.
  it("keeps unavailable models out of the selectable list", async () => {
    const unavailable = [
      {
        id: "cc-update-required-1",
        label: "Fable 5.1 (disabled)",
        reason: "Update to 2.1.255+ to use Fable 5.1",
      },
    ];
    initiateListModels.mockResolvedValue(
      request({
        status: "completed",
        models: catalog,
        unavailable_models: unavailable,
      }),
    );

    const result = await resolveRuntimeModels("rt-1");

    expect(result.models).toEqual(catalog);
    expect(result.models.map((m) => m.id)).not.toContain(
      "cc-update-required-1",
    );
    expect(result.unavailableModels).toEqual(unavailable);
  });

  // A daemon or server older than the field sends no key at all. That must read
  // as "nothing to warn about", never as a missing list the UI trips over.
  it("defaults unavailable models to empty when the backend omits them", async () => {
    initiateListModels.mockResolvedValue(
      request({ status: "completed", models: catalog }),
    );

    const result = await resolveRuntimeModels("rt-1");

    expect(result.unavailableModels).toEqual([]);
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

  // MUL-6606 review: the client budget has to cover the server's pending AND
  // running windows, which are sequential. A request the daemon claims at ~29s
  // (still inside modelListPendingTimeout) and then spends hermes' ~40s
  // discovery on reports at ~69s — while the server still holds the record
  // open. A 60s client budget gave up first and replaced the daemon's reason
  // with a generic "model discovery timed out", which is precisely the message
  // this issue exists to stop showing.
  it("waits out a claim near the pending limit plus a full discovery", async () => {
    vi.useFakeTimers();
    try {
      const start = Date.now();
      initiateListModels.mockResolvedValue(request({ status: "pending" }));
      getListModelsResult.mockImplementation(async () =>
        Date.now() - start >= 69_000
          ? request({ status: "completed", models: catalog })
          : request({ status: "running" }),
      );

      const pending = resolveRuntimeModels("rt-1");
      // Surface a rejection as the test failure instead of an unhandled one.
      const settled = pending.then(
        (value) => ({ ok: true as const, value }),
        (error: Error) => ({ ok: false as const, error }),
      );

      await vi.advanceTimersByTimeAsync(75_000);

      const outcome = await settled;
      if (!outcome.ok) {
        throw new Error(
          `client gave up while the server still held the request: ${outcome.error.message}`,
        );
      }
      expect(outcome.value.models).toEqual(catalog);
    } finally {
      vi.useRealTimers();
    }
  });

  // The budget is not unbounded: once BOTH server windows have elapsed the
  // record is terminal, so continuing to poll would spin forever against a
  // server that will never answer again.
  it("does eventually give up once the server's own windows have closed", async () => {
    vi.useFakeTimers();
    try {
      initiateListModels.mockResolvedValue(request({ status: "pending" }));
      getListModelsResult.mockResolvedValue(request({ status: "running" }));

      const pending = resolveRuntimeModels("rt-1");
      const settled = pending.then(
        () => ({ ok: true as const }),
        (error: Error) => ({ ok: false as const, error }),
      );

      await vi.advanceTimersByTimeAsync(200_000);

      const outcome = await settled;
      expect(outcome.ok).toBe(false);
      if (!outcome.ok) {
        expect(outcome.error.message).toContain("timed out");
      }
    } finally {
      vi.useRealTimers();
    }
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
    expect(staleTimeFor({ models: catalog, unavailableModels: [], supported: true, cached: true })).toBe(0);
  });

  it("trusts a live answer for the full window", () => {
    expect(staleTimeFor({ models: catalog, unavailableModels: [], supported: true, cached: false })).toBe(
      LIVE_MODELS_STALE_TIME_MS,
    );
    expect(staleTimeFor({ models: catalog, unavailableModels: [], supported: true })).toBe(
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

  it("force-refreshes the canonical cache instead of serving a cached catalog", async () => {
    initiateListModels.mockResolvedValue(
      request({ status: "completed", models: refreshedCatalog }),
    );

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    client.setQueryData(runtimeModelsKeys.forRuntime("rt-1"), {
      models: catalog,
      supported: true,
      cached: true,
    });

    await refreshRuntimeModels(client, "rt-1");

    expect(initiateListModels).toHaveBeenCalledWith("rt-1", { force: true });
    expect(client.getQueryData(runtimeModelsKeys.forRuntime("rt-1"))).toMatchObject({
      models: refreshedCatalog,
      cached: false,
    });
    client.clear();
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
