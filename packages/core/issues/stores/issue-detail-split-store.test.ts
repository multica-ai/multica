// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useIssueDetailSplitStore } from "./issue-detail-split-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
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

const KEY = "multica_issue_detail_split:acme";

describe("issue detail split store", () => {
  beforeEach(() => {
    useIssueDetailSplitStore.setState({ collapsed: false });
  });

  it("defaults to the expanded list rail (collapsed=false)", () => {
    expect(useIssueDetailSplitStore.getState().collapsed).toBe(false);
  });

  it("setCollapsed writes the value", () => {
    useIssueDetailSplitStore.getState().setCollapsed(true);
    expect(useIssueDetailSplitStore.getState().collapsed).toBe(true);

    useIssueDetailSplitStore.getState().setCollapsed(false);
    expect(useIssueDetailSplitStore.getState().collapsed).toBe(false);
  });

  it("toggleCollapsed flips between collapsed and expanded", () => {
    const { toggleCollapsed } = useIssueDetailSplitStore.getState();

    toggleCollapsed();
    expect(useIssueDetailSplitStore.getState().collapsed).toBe(true);

    toggleCollapsed();
    expect(useIssueDetailSplitStore.getState().collapsed).toBe(false);
  });

  it("restores a persisted collapsed value on workspace rehydration", async () => {
    localStorage.setItem(
      KEY,
      JSON.stringify({ state: { collapsed: true }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    expect(useIssueDetailSplitStore.getState().collapsed).toBe(true);
  });

  it("falls back to the expanded default when persisted state lacks the key", async () => {
    localStorage.setItem(
      KEY,
      JSON.stringify({ state: {}, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    expect(useIssueDetailSplitStore.getState().collapsed).toBe(false);
  });
});

afterEach(() => {
  setCurrentWorkspace(null, null);
  localStorage.clear();
});
