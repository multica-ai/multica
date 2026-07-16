// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useIssueLinkStore } from "./issue-link-store";

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.setItem !== "function") {
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

describe("issue link store", () => {
  beforeEach(() => {
    useIssueLinkStore.setState({ openInNewTab: true });
  });

  it("defaults to opening issue links in a new tab", () => {
    expect(useIssueLinkStore.getState().openInNewTab).toBe(true);
  });

  it("setOpenInNewTab switches to in-place navigation and back", () => {
    useIssueLinkStore.getState().setOpenInNewTab(false);
    expect(useIssueLinkStore.getState().openInNewTab).toBe(false);

    useIssueLinkStore.getState().setOpenInNewTab(true);
    expect(useIssueLinkStore.getState().openInNewTab).toBe(true);
  });
});
