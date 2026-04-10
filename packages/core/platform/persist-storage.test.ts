import { describe, it, expect, vi } from "vitest";
import { createPersistStorage } from "./persist-storage";
import type { StorageAdapter } from "../types/storage";

function mockAdapter(): StorageAdapter {
  const store = new Map<string, string>();
  return {
    getItem: vi.fn((k) => store.get(k) ?? null),
    setItem: vi.fn((k, v) => store.set(k, v)),
    removeItem: vi.fn((k) => store.delete(k)),
  };
}

describe("createPersistStorage", () => {
  it("delegates to StorageAdapter", () => {
    const adapter = mockAdapter();
    const storage = createPersistStorage(adapter);

    storage.setItem("key", JSON.stringify("value"));
    expect(adapter.setItem).toHaveBeenCalledWith(
      "key",
      JSON.stringify("value"),
    );

    const result = storage.getItem("key");
    expect(adapter.getItem).toHaveBeenCalledWith("key");
    expect(result).toEqual(JSON.stringify("value"));
  });

  it("returns null for missing keys", () => {
    const adapter = mockAdapter();
    const storage = createPersistStorage(adapter);

    const result = storage.getItem("nonexistent");
    expect(result).toBeNull();
  });

  it("removeItem delegates correctly", () => {
    const adapter = mockAdapter();
    const storage = createPersistStorage(adapter);

    storage.removeItem("key");
    expect(adapter.removeItem).toHaveBeenCalledWith("key");
  });
});
