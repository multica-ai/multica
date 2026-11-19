// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import type { StorageAdapter } from "../types/storage";
import { defaultStorage, installDefaultStorageBackend } from "./storage";

function memoryAdapter(): Required<StorageAdapter> & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => void data.set(key, value),
    removeItem: (key) => void data.delete(key),
    keys: () => [...data.keys()],
  };
}

describe("defaultStorage", () => {
  afterEach(() => {
    localStorage.clear();
  });

  it("reads and writes Web Storage when no backend is installed", () => {
    defaultStorage.setItem("multica_theme", "dark");
    expect(localStorage.getItem("multica_theme")).toBe("dark");
    defaultStorage.removeItem("multica_theme");
    expect(localStorage.getItem("multica_theme")).toBeNull();
  });

  it("routes every operation to an installed backend instead of Web Storage", () => {
    const backend = memoryAdapter();
    installDefaultStorageBackend(backend);

    defaultStorage.setItem("multica_theme", "dark");
    expect(backend.data.get("multica_theme")).toBe("dark");
    expect(localStorage.getItem("multica_theme")).toBeNull();
    expect(defaultStorage.getItem("multica_theme")).toBe("dark");
    expect(defaultStorage.keys?.()).toEqual(["multica_theme"]);

    defaultStorage.removeItem("multica_theme");
    expect(backend.data.size).toBe(0);
  });
});
