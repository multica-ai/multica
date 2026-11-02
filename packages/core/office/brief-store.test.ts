// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import { useOfficeBriefStore } from "./brief-store";

// Node 25 ships a partial `localStorage` shim under jsdom; use a real in-memory Storage.
beforeAll(() => {
  if (typeof globalThis.localStorage?.setItem !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() {
        return values.size;
      },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => {
        values.delete(k);
      },
      setItem: (k, v) => {
        values.set(k, v);
      },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

describe("office brief store", () => {
  beforeEach(() => {
    setCurrentWorkspace("acme", "ws-1");
    useOfficeBriefStore.setState({ lastSeenBriefId: null });
  });

  it("remembers the newest brief the viewer opened", () => {
    useOfficeBriefStore.getState().markBriefSeen("brief-7");
    expect(useOfficeBriefStore.getState().lastSeenBriefId).toBe("brief-7");
  });

  it("keeps the pointer under the workspace's own storage namespace", () => {
    useOfficeBriefStore.getState().markBriefSeen("brief-7");
    const keys = Array.from({ length: localStorage.length }, (_, i) => localStorage.key(i) ?? "");
    expect(keys.some((key) => key.includes("multica_office_brief") && key.includes("acme"))).toBe(true);
  });
});
