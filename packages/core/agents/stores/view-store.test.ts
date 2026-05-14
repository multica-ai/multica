// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useAgentsViewStore } from "./view-store";
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
  useAgentsViewStore.setState({ scope: "all" });
  setCurrentWorkspace(null, null);
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useAgentsViewStore", () => {
  it("defaults to 'all' (fork #88)", () => {
    expect(useAgentsViewStore.getState().scope).toBe("all");
  });

  it("setScope mutates the store", () => {
    useAgentsViewStore.getState().setScope("mine");
    expect(useAgentsViewStore.getState().scope).toBe("mine");
  });

  it("partialize persists only scope under the workspace-namespaced key", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    useAgentsViewStore.getState().setScope("mine");

    const raw = localStorage.getItem("multica_agents_view:acme");
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(parsed.state).toEqual({ scope: "mine" });
  });

  it("rehydrates a different saved scope on workspace switch", async () => {
    localStorage.setItem(
      "multica_agents_view:acme",
      JSON.stringify({ state: { scope: "mine" }, version: 0 }),
    );
    localStorage.setItem(
      "multica_agents_view:beta",
      JSON.stringify({ state: { scope: "all" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useAgentsViewStore.getState().scope).toBe("mine");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useAgentsViewStore.getState().scope).toBe("all");
  });

  it("resets to 'all' when switching to a workspace with no persisted value", async () => {
    localStorage.setItem(
      "multica_agents_view:acme",
      JSON.stringify({ state: { scope: "mine" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useAgentsViewStore.getState().scope).toBe("mine");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useAgentsViewStore.getState().scope).toBe("all");
    expect(localStorage.getItem("multica_agents_view:acme")).not.toBeNull();
  });
});
