// @vitest-environment node
import { beforeEach, expect, it, vi } from "vitest";
import { clearImportedModelPrices, readLegacyModelPrices } from "./pricing";

const storage = vi.hoisted(() => new Map<string, string>());
vi.mock("../platform/storage", () => ({ defaultStorage: {
  getItem: (key: string) => storage.get(key) ?? null,
  setItem: (key: string, value: string) => storage.set(key, value),
  removeItem: (key: string) => storage.delete(key),
} }));
const key = "multica_runtime_custom_pricing";
const rate = { input: 1, output: 2, cacheRead: 0, cacheWrite: 1 };
beforeEach(() => storage.clear());
it("previews valid prices while preserving malformed legacy entries", () => {
  storage.set(key, JSON.stringify({ version: 0, state: { pricings: { good: rate, malformed: { input: null } } } }));
  expect(readLegacyModelPrices()).toEqual({ good: rate });
  clearImportedModelPrices({ good: rate });
  expect(JSON.parse(storage.get(key)!).state.pricings).toEqual({ malformed: { input: null } });
});
it("does not touch local data on an ordinary workspace save", () => {
  storage.set(key, "old invalid data");
  clearImportedModelPrices({});
  expect(storage.get(key)).toBe("old invalid data");
});
it("clears only imported entries unchanged since preview", () => {
  storage.set(key, JSON.stringify({ state: { pricings: { imported: rate, changed: { ...rate, input: 9 }, other: rate } } }));
  clearImportedModelPrices({ imported: rate, changed: rate });
  expect(readLegacyModelPrices()).toEqual({ changed: { ...rate, input: 9 }, other: rate });
});
