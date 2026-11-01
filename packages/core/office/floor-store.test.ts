// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import { useOfficeFloorStore } from "./floor-store";

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

describe("office floor store", () => {
  beforeEach(() => {
    setCurrentWorkspace("acme", "ws-1");
    useOfficeFloorStore.setState({ lastFloorId: null });
  });

  it("remembers the floor", () => {
    useOfficeFloorStore.getState().setLastFloorId("floor-2");
    expect(useOfficeFloorStore.getState().lastFloorId).toBe("floor-2");
  });

  it("keeps the floor under the workspace's own storage namespace", () => {
    useOfficeFloorStore.getState().setLastFloorId("floor-2");
    const keys = Array.from({ length: localStorage.length }, (_, i) => localStorage.key(i) ?? "");
    expect(keys.some((key) => key.includes("multica_office_floor") && key.includes("acme"))).toBe(true);
  });
});
