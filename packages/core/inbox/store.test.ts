// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useInboxSortStore } from "./store";
import { setCurrentWorkspace } from "../platform/workspace-storage";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

// Node 26 jsdom ships a partial localStorage shim that is missing clear/removeItem.
// Replace it with a full in-memory Storage so persist can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.clear !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => { values.delete(k); },
      setItem: (k, v) => { values.set(k, v); },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

beforeEach(() => {
  localStorage.clear();
  // Reset to defaults between tests
  useInboxSortStore.setState({ sortField: "date", sortDirection: "desc" });
  setCurrentWorkspace(null, null);
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useInboxSortStore", () => {
  it("defaults to date-desc (the pre-change ordering)", () => {
    expect(useInboxSortStore.getState().sortField).toBe("date");
    expect(useInboxSortStore.getState().sortDirection).toBe("desc");
  });

  it("setSortField switches field and resets to that field's default direction", () => {
    useInboxSortStore.getState().setSortField("priority");
    expect(useInboxSortStore.getState().sortField).toBe("priority");
    expect(useInboxSortStore.getState().sortDirection).toBe("desc");

    useInboxSortStore.getState().setSortField("unread");
    expect(useInboxSortStore.getState().sortField).toBe("unread");
    expect(useInboxSortStore.getState().sortDirection).toBe("desc");
  });

  it("setSortDirection updates direction without changing the field", () => {
    useInboxSortStore.getState().setSortField("priority");
    useInboxSortStore.getState().setSortDirection("asc");
    expect(useInboxSortStore.getState().sortField).toBe("priority");
    expect(useInboxSortStore.getState().sortDirection).toBe("asc");
  });

  it("toggleSort flips direction when the same field is re-selected", () => {
    useInboxSortStore.getState().toggleSort("date");
    expect(useInboxSortStore.getState().sortDirection).toBe("asc");
    useInboxSortStore.getState().toggleSort("date");
    expect(useInboxSortStore.getState().sortDirection).toBe("desc");
  });

  it("persists sort choice under the workspace-namespaced key", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    useInboxSortStore.getState().setSortField("priority");

    const raw = localStorage.getItem("multica_inbox_sort:acme");
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(parsed.state.sortField).toBe("priority");
    expect(parsed.state.sortDirection).toBe("desc");
  });

  it("persists sortDirection change under the workspace key", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    useInboxSortStore.getState().setSortField("priority");
    useInboxSortStore.getState().setSortDirection("asc");

    const raw = localStorage.getItem("multica_inbox_sort:acme");
    const parsed = JSON.parse(raw as string);
    expect(parsed.state.sortField).toBe("priority");
    expect(parsed.state.sortDirection).toBe("asc");
  });

  it("rehydrates the saved sort choice on workspace switch (sort persists across reload)", async () => {
    localStorage.setItem(
      "multica_inbox_sort:acme",
      JSON.stringify({ state: { sortField: "priority", sortDirection: "desc" }, version: 0 }),
    );
    localStorage.setItem(
      "multica_inbox_sort:beta",
      JSON.stringify({ state: { sortField: "unread", sortDirection: "desc" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useInboxSortStore.getState().sortField).toBe("priority");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useInboxSortStore.getState().sortField).toBe("unread");
  });

  it("falls back to date-desc when switching to a workspace with no persisted sort", async () => {
    localStorage.setItem(
      "multica_inbox_sort:acme",
      JSON.stringify({ state: { sortField: "priority", sortDirection: "desc" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useInboxSortStore.getState().sortField).toBe("priority");

    setCurrentWorkspace("beta-fresh", "ws_b");
    await flush();
    await flush();
    expect(useInboxSortStore.getState().sortField).toBe("date");
    expect(useInboxSortStore.getState().sortDirection).toBe("desc");
  });
});
