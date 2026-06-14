import { describe, expect, it, beforeEach, afterEach } from "vitest";
import {
  useManifestPricingStore,
  getManifestPricing,
} from "./manifest-pricing-store";

beforeEach(() => {
  useManifestPricingStore.setState({ pricings: {} });
});

afterEach(() => {
  useManifestPricingStore.setState({ pricings: {} });
});

describe("manifest-pricing-store", () => {
  it("returns undefined when nothing is registered", () => {
    expect(getManifestPricing("anything")).toBeUndefined();
  });

  it("setManifestPricings replaces the whole map", () => {
    useManifestPricingStore.getState().setManifestPricings({
      "model-a": { input: 1, output: 2 },
    });
    useManifestPricingStore.getState().setManifestPricings({
      "model-b": { input: 3, output: 4 },
    });
    expect(getManifestPricing("model-a")).toBeUndefined();
    expect(getManifestPricing("model-b")).toEqual({ input: 3, output: 4 });
  });

  it("mergeManifestPricings keeps existing entries", () => {
    useManifestPricingStore.getState().setManifestPricings({
      "model-a": { input: 1, output: 2 },
    });
    useManifestPricingStore.getState().mergeManifestPricings({
      "model-b": { input: 3, output: 4 },
    });
    expect(getManifestPricing("model-a")).toEqual({ input: 1, output: 2 });
    expect(getManifestPricing("model-b")).toEqual({ input: 3, output: 4 });
  });

  it("survives partial pricing fields without crashing the consumer", () => {
    // Manifest authors can ship just {input,output}; the consumer
    // (resolvePricing) is responsible for zero-filling the missing
    // cache rates. The store itself stores whatever the manifest
    // declared — verbatim.
    useManifestPricingStore.getState().setManifestPricings({
      "model-c": { input: 0.5, output: 1.5 },
    });
    const pricing = getManifestPricing("model-c");
    expect(pricing?.input).toBe(0.5);
    expect(pricing?.output).toBe(1.5);
    expect(pricing?.cacheRead).toBeUndefined();
    expect(pricing?.cacheWrite).toBeUndefined();
  });
});
