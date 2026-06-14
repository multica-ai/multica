import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import type { RuntimeModelListRequest } from "../types/agent";
import {
  getManifestPricing,
  useManifestPricingStore,
} from "./manifest-pricing-store";
import { resolveRuntimeModels } from "./models";

vi.mock("../api", () => ({
  api: {
    initiateListModels: vi.fn(),
    getListModelsResult: vi.fn(),
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
  useManifestPricingStore.setState({ pricings: {} });
});

describe("resolveRuntimeModels", () => {
  it("merges dynamic discovery pricing into the manifest pricing store", async () => {
    const completed: RuntimeModelListRequest = {
      id: "req-1",
      runtime_id: "rt-1",
      status: "completed",
      models: [{ id: "dynamic-model", label: "Dynamic", provider: "external" }],
      pricing: {
        "dynamic-model": { input: 3, output: 15, cacheRead: 0.3 },
      },
      supported: true,
      created_at: "2026-06-10T00:00:00Z",
      updated_at: "2026-06-10T00:00:00Z",
    };

    vi.mocked(api.initiateListModels).mockResolvedValue(completed);

    const result = await resolveRuntimeModels("rt-1");

    expect(result.pricing?.["dynamic-model"]).toEqual({
      input: 3,
      output: 15,
      cacheRead: 0.3,
    });
    expect(getManifestPricing("dynamic-model")).toEqual({
      input: 3,
      output: 15,
      cacheRead: 0.3,
    });
    expect(api.getListModelsResult).not.toHaveBeenCalled();
  });
});
