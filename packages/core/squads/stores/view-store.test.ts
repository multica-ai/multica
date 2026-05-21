// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useSquadsViewStore } from "./view-store";
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

beforeEach(() => {
  localStorage.clear();
  useSquadsViewStore.setState({ scope: "mine" });
  setCurrentWorkspace(null, null);
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useSquadsViewStore", () => {
  it("defaults to 'mine'", () => {
    expect(useSquadsViewStore.getState().scope).toBe("mine");
  });

  it("setScope mutates the store", () => {
    useSquadsViewStore.getState().setScope("all");
    expect(useSquadsViewStore.getState().scope).toBe("all");
  });

  it("partialize persists only scope under the workspace-namespaced key", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    useSquadsViewStore.getState().setScope("all");

    const raw = localStorage.getItem("multica_squads_view:acme");
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(parsed.state).toEqual({ scope: "all" });
  });

  it("rehydrates a different saved scope on workspace switch", async () => {
    localStorage.setItem(
      "multica_squads_view:acme",
      JSON.stringify({ state: { scope: "all" }, version: 0 }),
    );
    localStorage.setItem(
      "multica_squads_view:beta",
      JSON.stringify({ state: { scope: "mine" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useSquadsViewStore.getState().scope).toBe("all");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useSquadsViewStore.getState().scope).toBe("mine");
  });

  it("resets to 'mine' when switching to a workspace with no persisted value", async () => {
    localStorage.setItem(
      "multica_squads_view:acme",
      JSON.stringify({ state: { scope: "all" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useSquadsViewStore.getState().scope).toBe("all");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useSquadsViewStore.getState().scope).toBe("mine");
    expect(localStorage.getItem("multica_squads_view:acme")).not.toBeNull();
  });
});
