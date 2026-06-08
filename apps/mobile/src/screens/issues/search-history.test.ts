import { describe, expect, it } from "vitest";
import {
  addSearchHistoryItem,
  clearSearchHistory,
  getSearchHistoryStorageKey,
  readSearchHistory,
  removeSearchHistoryItem,
  SEARCH_HISTORY_LIMIT,
  type SearchHistoryStorage,
} from "./search-history";

function createStorage(initial?: Record<string, string>): SearchHistoryStorage {
  const values = new Map(Object.entries(initial ?? {}));
  return {
    getItem: (key) => values.get(key) ?? null,
    removeItem: (key) => {
      values.delete(key);
    },
    setItem: (key, value) => {
      values.set(key, value);
    },
  };
}

describe("mobile search history", () => {
  it("keeps the latest unique query first and caps history at ten items", () => {
    const storage = createStorage();
    for (let index = 0; index < SEARCH_HISTORY_LIMIT + 2; index += 1) {
      addSearchHistoryItem(storage, "workspace-1", `query ${index}`);
    }

    const next = addSearchHistoryItem(storage, "workspace-1", "query 8");

    expect(next).toHaveLength(SEARCH_HISTORY_LIMIT);
    expect(next[0]).toBe("query 8");
    expect(next.filter((item) => item === "query 8")).toHaveLength(1);
    expect(next).not.toContain("query 0");
  });

  it("isolates history by workspace", () => {
    const storage = createStorage();
    addSearchHistoryItem(storage, "workspace-1", "alpha");
    addSearchHistoryItem(storage, "workspace-2", "beta");

    expect(readSearchHistory(storage, "workspace-1")).toEqual(["alpha"]);
    expect(readSearchHistory(storage, "workspace-2")).toEqual(["beta"]);
  });

  it("removes single items and clears the workspace history", () => {
    const storage = createStorage();
    addSearchHistoryItem(storage, "workspace-1", "alpha");
    addSearchHistoryItem(storage, "workspace-1", "beta");

    expect(removeSearchHistoryItem(storage, "workspace-1", "alpha")).toEqual(["beta"]);
    expect(clearSearchHistory(storage, "workspace-1")).toEqual([]);
    expect(readSearchHistory(storage, "workspace-1")).toEqual([]);
  });

  it("falls back to an empty list for invalid stored data", () => {
    const storage = createStorage({
      [getSearchHistoryStorageKey("workspace-1")]: "{bad json",
    });

    expect(readSearchHistory(storage, "workspace-1")).toEqual([]);
  });
});
