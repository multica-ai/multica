// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { ApiClient } from "../api/client";
import {
  BUNDLED_PRICING,
  modelPricingSnapshotSchema,
  modelPricingRowToWire,
  resolveModelPricing,
} from "./pricing";
import { modelPricingKey, modelPricingOptions } from "./pricing-queries";

afterEach(() => vi.unstubAllGlobals());
const snapshot = {
  ...BUNDLED_PRICING,
  version: "test",
  revision: 0,
  canManage: false,
  checkedAt: null,
  succeededAt: null,
  lastError: "",
  timezone: "UTC",
};
const wireSnapshot = {
  version: snapshot.version,
  rows: Object.fromEntries(Object.entries(snapshot.rows).map(([key, row]) => [key, modelPricingRowToWire(row)])),
  aliases: snapshot.aliases,
  overrides: {},
  revision: snapshot.revision,
  can_manage: snapshot.canManage,
  checked_at: snapshot.checkedAt,
  succeeded_at: snapshot.succeededAt,
  last_error: snapshot.lastError,
  timezone: snapshot.timezone,
};
describe("workspace model prices", () => {
  it("converts wire prices and provenance to camelCase at the API boundary", () => {
    const raw = {
      ...wireSnapshot,
      can_manage: true,
      rows: {
        "vendor/model": {
          input: 1,
          output: 4,
          cache_read: 0.0028,
          cache_write: 1.25,
          provider: "vendor",
          model: "model",
          source: "litellm",
          source_url: "https://example.test/pricing",
        },
      },
    };
    const parsed = modelPricingSnapshotSchema.parse(raw);
    expect(parsed.canManage).toBe(true);
    expect(parsed.rows["vendor/model"]).toEqual({
      input: 1,
      output: 4,
      cacheRead: 0.0028,
      cacheWrite: 1.25,
      provider: "vendor",
      model: "model",
      source: "litellm",
      sourceUrl: "https://example.test/pricing",
    });
    expect(parsed).not.toHaveProperty("can_manage");
  });

  it("keeps a known subscription equivalent distinct from a free API", () => {
    expect(resolveModelPricing("kimi-code/k3-256k", "hermes")?.input).toBe(3);
    expect(resolveModelPricing("glm-4.7-flash")?.input).toBe(0);
    expect(resolveModelPricing("qwen3.8-max-preview")).toBeUndefined();
  });

  it.each([
    "claude-fable-5-1[1m]",
    "claude-fable-5.1-20260825[1m]",
    "custom:anthropic/claude-fable-5.1-latest[1m]",
    "anthropic:claude-fable-5.1[128k]",
  ])("resolves one context tag and nested routing in %s", (model) => {
    expect(resolveModelPricing(model, "hermes")).toEqual(
      BUNDLED_PRICING.rows["claude-fable-5-1"],
    );
  });

  it.each([
    "claude-fable-5-1[1m][2m]",
    "custom:anthropic/claude-fable-5.1[1m][2m]",
    "claude-fable-5-1[1m]junk",
    "claude-fable-5-1[]",
    "claude-fable-5-1[",
    "claude-fable-5-1-latest-preview",
  ])("does not borrow the Fable rate for an unknown variant %s", (model) => {
    expect(resolveModelPricing(model, "hermes")).toBeUndefined();
  });

  it("resolves the exact Ark GLM Flash alias using synced rates", () => {
    const rate = { input: 0.15, output: 0.5, cacheRead: 0.03, cacheWrite: 0 };
    const context = {
      rows: { "zai/glm-5.3-flash": rate },
      aliases: {
        ...BUNDLED_PRICING.aliases,
        "glm-5.3-flash": "zai/glm-5.3-flash",
      },
      overrides: {},
    };
    for (const model of [
      "glm-5-3-flash",
      "ark-coding-plan/glm-5-3-flash",
      "custom:ark-coding-plan/glm-5-3-flash",
    ]) {
      expect(resolveModelPricing(model, "omp", context)).toEqual(rate);
    }
    expect(
      resolveModelPricing("glm-5-3-flash-extra", "omp", context),
    ).toBeUndefined();
    const custom = {
      ...context,
      overrides: { "glm-5.3-flash": { ...rate, input: 7 } },
    };
    expect(resolveModelPricing("glm-5-3-flash", "omp", custom)?.input).toBe(7);
    const qualified = {
      ...context,
      rows: {
        ...context.rows,
        "ark-coding-plan/glm-5-3-flash": { ...rate, input: 9 },
      },
    };
    expect(
      resolveModelPricing("ark-coding-plan/glm-5-3-flash", "omp", qualified)?.input,
    ).toBe(9);
  });
  it("resolves nested subscription identities without a model call", () => {
    const api = resolveModelPricing("kimi-k3");
    expect(api?.input).toBeGreaterThan(0);
    expect(resolveModelPricing("custom:kimi-code/k3-256k", "hermes")).toEqual(
      api,
    );
    expect(resolveModelPricing("k3-256k-other", "hermes")).toBeUndefined();
  });
  it("follows aliases while preserving serving-provider prices and canonical overrides", () => {
    const context = {
      rows: {
        "moonshotai/kimi-k3": {
          input: 3,
          output: 15,
          cacheRead: 0.3,
          cacheWrite: 3,
        },
        "other/kimi-k3": { input: 8, output: 20, cacheRead: 1, cacheWrite: 8 },
      },
      aliases: { "k3-256k": "kimi-k3", "kimi-k3": "moonshotai/kimi-k3" },
      overrides: {},
    };
    expect(resolveModelPricing("k3-256k", "hermes", context)?.input).toBe(3);
    expect(resolveModelPricing("other/kimi-k3", "hermes", context)?.input).toBe(
      8,
    );
    const custom = {
      ...context,
      overrides: {
        "kimi-k3": { input: 7, output: 7, cacheRead: 7, cacheWrite: 7 },
      },
    };
    expect(resolveModelPricing("k3-256k", "hermes", custom)?.input).toBe(7);
    expect(resolveModelPricing("k3-256k", "hermes", context)?.input).toBe(3);
  });

  it.each([
    "reseller/gpt-5.6-sol",
    "reseller:gpt-5.6-sol",
    "custom:reseller:gpt-5.6-sol",
    "hermes/custom:reseller:gpt-5.6-sol",
    "reseller:gpt-5.6-sol[1m]",
  ])("preserves the explicit serving provider in %s", (model) => {
    const reseller = { input: 9, output: 30, cacheRead: 0.9, cacheWrite: 9 };
    const override = { input: 12, output: 40, cacheRead: 1.2, cacheWrite: 12 };
    const context = {
      rows: {
        "reseller/gpt-5.6-sol": reseller,
        "openai/gpt-5.6-sol": { input: 4, output: 20, cacheRead: 0.4, cacheWrite: 5 },
      },
      aliases: { "gpt-5.6-sol": "openai/gpt-5.6-sol" },
      overrides: {},
    };
    expect(resolveModelPricing(model, "hermes", context)).toEqual(reseller);
    expect(resolveModelPricing(model, "hermes", {
      ...context,
      overrides: { "reseller/gpt-5.6-sol": override },
    })).toEqual(override);
  });

  it.each(["hermes", "reseller"])("keeps an exact colon key first for provider %s", (provider) => {
    const exact = { input: 15, output: 30, cacheRead: 1.5, cacheWrite: 15 };
    const context = {
      rows: {
        "reseller:gpt-5.6-sol": exact,
        "reseller/gpt-5.6-sol": { ...exact, input: 9 },
      },
      aliases: {},
      overrides: {},
    };
    expect(resolveModelPricing("reseller:gpt-5.6-sol", provider, context)).toEqual(exact);
    expect(resolveModelPricing("reseller:gpt-5.6-sol", provider, {
      ...context,
      overrides: context.rows,
    })).toEqual(exact);
  });

  it.each([
    null,
    {},
    { ...wireSnapshot, overrides: null },
    {
      ...wireSnapshot,
      rows: { x: { input: null, output: 1, cacheRead: 0, cacheWrite: 0 } },
    },
    { ...wireSnapshot, revision: -1 },
    { ...wireSnapshot, can_manage: "true" },
  ])(
    "rejects malformed responses without replacing a good cached price",
    async (body) => {
      expect(modelPricingSnapshotSchema.safeParse(body).success).toBe(false);
      vi.stubGlobal(
        "fetch",
        vi.fn(
          async () =>
            new Response(JSON.stringify(body), {
              status: 200,
              headers: { "Content-Type": "application/json" },
            }),
        ),
      );
      const client = new ApiClient("https://example.test");
      const query = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      query.setQueryData(modelPricingKey("a"), snapshot);
      await expect(
        query.fetchQuery({
          queryKey: modelPricingKey("a"),
          queryFn: () => client.getModelPricing("a"),
          staleTime: 0,
        }),
      ).rejects.toThrow();
      expect(query.getQueryData(modelPricingKey("a"))).toEqual(snapshot);
      expect(query.getQueryData(modelPricingKey("b"))).toBeUndefined();
      query.clear();
    },
  );
  it("isolates workspace query keys and does not poll price sources from clients", () => {
    expect(modelPricingOptions("a").queryKey).not.toEqual(
      modelPricingOptions("b").queryKey,
    );
    expect(modelPricingOptions("a")).not.toHaveProperty("refetchInterval");
  });
  it("parses the same shared snapshot for GET, save and refresh", async () => {
    const fetch = vi.fn(
      async () =>
        new Response(JSON.stringify(wireSnapshot), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetch);
    const client = new ApiClient("https://example.test");
    expect(await client.getModelPricing("a")).toEqual(snapshot);
    expect(
      await client.saveModelPricing("a", { revision: 0, overrides: {} }),
    ).toEqual(snapshot);
    expect(await client.refreshModelPricing("a")).toEqual(snapshot);
    expect(fetch.mock.calls).toHaveLength(3);
  });

  it("sends the workspace revision and all four override rates in wire format", async () => {
    const fetch = vi.fn(async (_url: unknown, _init?: RequestInit) =>
      new Response(JSON.stringify(wireSnapshot), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);
    const client = new ApiClient("https://example.test");
    await client.saveModelPricing("workspace-a", {
      revision: 7,
      overrides: {
        "vendor/model": { input: 1, output: 4, cacheRead: 0.0028, cacheWrite: 1.25 },
      },
    });
    const [url, init] = fetch.mock.calls[0]!;
    expect(url).toBe("https://example.test/api/workspaces/workspace-a/model-pricing");
    expect(init?.method).toBe("PUT");
    expect(JSON.parse(String(init?.body))).toEqual({
      revision: 7,
      overrides: {
        "vendor/model": { input: 1, output: 4, cache_read: 0.0028, cache_write: 1.25 },
      },
    });
  });
});
