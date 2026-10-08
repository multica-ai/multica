import { describe, expect, it } from "vitest";
import type { StorageAdapter } from "../types/storage";
import { getOrCreateInstallId } from "./install-id";

function memoryStorage(): StorageAdapter {
  const values = new Map<string, string>();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
}

describe("getOrCreateInstallId", () => {
  it("persists one UUID across calls", () => {
    const storage = memoryStorage();
    const generated = "3d267c82-59a9-46b2-a189-56e1504449a7";
    expect(getOrCreateInstallId(storage, () => generated)).toBe(generated);
    expect(getOrCreateInstallId(storage, () => "unused")).toBe(generated);
  });

  it("replaces corrupted persisted values", () => {
    const storage = memoryStorage();
    storage.setItem("multica_install_id", "broken");
    const generated = "c9c7a898-ee7c-44a0-bd0c-66a16461e591";
    expect(getOrCreateInstallId(storage, () => generated)).toBe(generated);
  });
});
