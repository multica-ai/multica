// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import {
  useListDetailSplitStore,
  LIST_DETAIL_RAIL_MIN_SIZE,
  LIST_DETAIL_RAIL_MAX_SIZE,
} from "./list-detail-split-store";
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

const KEY = "multica_list_detail_split:acme";

describe("list detail split store", () => {
  beforeEach(() => {
    useListDetailSplitStore.setState({ collapsed: true, size: undefined });
  });

  it("defaults to the collapsed rail (collapsed=true)", () => {
    expect(useListDetailSplitStore.getState().collapsed).toBe(true);
    expect(useListDetailSplitStore.getState().size).toBeUndefined();
  });

  it("setCollapsed writes the value", () => {
    useListDetailSplitStore.getState().setCollapsed(false);
    expect(useListDetailSplitStore.getState().collapsed).toBe(false);

    useListDetailSplitStore.getState().setCollapsed(true);
    expect(useListDetailSplitStore.getState().collapsed).toBe(true);
  });

  it("toggleCollapsed flips between collapsed and expanded", () => {
    const { toggleCollapsed } = useListDetailSplitStore.getState();

    toggleCollapsed();
    expect(useListDetailSplitStore.getState().collapsed).toBe(false);

    toggleCollapsed();
    expect(useListDetailSplitStore.getState().collapsed).toBe(true);
  });

  it("setSize stores the pixel width", () => {
    useListDetailSplitStore.getState().setSize(320);
    expect(useListDetailSplitStore.getState().size).toBe(320);
  });

  it("setSize clamps out-of-range values to the 240-480 pixel bounds", () => {
    useListDetailSplitStore.getState().setSize(100);
    expect(useListDetailSplitStore.getState().size).toBe(LIST_DETAIL_RAIL_MIN_SIZE);

    useListDetailSplitStore.getState().setSize(600);
    expect(useListDetailSplitStore.getState().size).toBe(LIST_DETAIL_RAIL_MAX_SIZE);

    useListDetailSplitStore.getState().setSize(239.6);
    expect(useListDetailSplitStore.getState().size).toBe(LIST_DETAIL_RAIL_MIN_SIZE);

    useListDetailSplitStore.getState().setSize(479.6);
    expect(useListDetailSplitStore.getState().size).toBe(LIST_DETAIL_RAIL_MAX_SIZE);
  });

  it("restores a persisted collapsed value and clamps the persisted size on workspace rehydration", async () => {
    localStorage.setItem(
      KEY,
      JSON.stringify({
        state: { collapsed: false, size: 700 },
        version: 0,
      }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    expect(useListDetailSplitStore.getState().collapsed).toBe(false);
    expect(useListDetailSplitStore.getState().size).toBe(LIST_DETAIL_RAIL_MAX_SIZE);
  });

  it("falls back to the collapsed default when persisted state lacks the key", async () => {
    localStorage.setItem(
      KEY,
      JSON.stringify({ state: {}, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    expect(useListDetailSplitStore.getState().collapsed).toBe(true);
    expect(useListDetailSplitStore.getState().size).toBeUndefined();
  });
});

afterEach(() => {
  setCurrentWorkspace(null, null);
  localStorage.clear();
});
