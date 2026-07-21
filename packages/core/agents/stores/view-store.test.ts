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
  useAgentsViewStore.setState({ scope: "mine" });
  setCurrentWorkspace(null, null);
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useAgentsViewStore", () => {
  it("defaults to 'mine'", () => {
    expect(useAgentsViewStore.getState().scope).toBe("mine");
  });

  it("setScope mutates the store", () => {
    useAgentsViewStore.getState().setScope("all");
    expect(useAgentsViewStore.getState().scope).toBe("all");
  });

  it("partialize persists only view prefs (no actions) under the workspace-namespaced key", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    useAgentsViewStore.getState().setScope("all");

    const raw = localStorage.getItem("multica_agents_view:acme");
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(Object.keys(parsed.state).sort()).toEqual([
      "filters",
      "hiddenColumns",
      "scope",
      "sortDirection",
      "sortField",
    ]);
    expect(parsed.state.scope).toBe("all");
  });

  it("rehydrates a different saved scope on workspace switch", async () => {
    localStorage.setItem(
      "multica_agents_view:acme",
      JSON.stringify({ state: { scope: "all" }, version: 0 }),
    );
    localStorage.setItem(
      "multica_agents_view:beta",
      JSON.stringify({ state: { scope: "mine" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useAgentsViewStore.getState().scope).toBe("all");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useAgentsViewStore.getState().scope).toBe("mine");
  });

  it("resets to 'mine' when switching to a workspace with no persisted value", async () => {
    localStorage.setItem(
      "multica_agents_view:acme",
      JSON.stringify({ state: { scope: "all" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useAgentsViewStore.getState().scope).toBe("all");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useAgentsViewStore.getState().scope).toBe("mine");
    expect(localStorage.getItem("multica_agents_view:acme")).not.toBeNull();
  });

  it("backfills new filter dimensions when rehydrating a pre-owners payload", async () => {
    // A payload persisted before the `owners` filter existed must not drop
    // the key to undefined (the agents list filter predicate reads
    // `filters.owners.length` and would crash).
    localStorage.setItem(
      "multica_agents_view:acme",
      JSON.stringify({
        state: { filters: { availability: ["online"], runtimes: [] } },
        version: 0,
      }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    const filters = useAgentsViewStore.getState().filters;
    expect(filters.owners).toEqual([]);
    expect(filters.availability).toEqual(["online"]);
  });

  describe("access filter dimension", () => {
    it("EMPTY_AGENT_FILTERS initializes access to []", async () => {
      const { EMPTY_AGENT_FILTERS } = await import("./view-store");
      expect(EMPTY_AGENT_FILTERS.access).toEqual([]);
    });

    it("toggleFilter('access', value) adds and removes the value", () => {
      const { toggleFilter } = useAgentsViewStore.getState();
      toggleFilter("access", "owner-only");
      expect(useAgentsViewStore.getState().filters.access).toEqual(["owner-only"]);
      toggleFilter("access", "workspace");
      expect(useAgentsViewStore.getState().filters.access).toEqual([
        "owner-only",
        "workspace",
      ]);
      toggleFilter("access", "owner-only");
      expect(useAgentsViewStore.getState().filters.access).toEqual(["workspace"]);
    });

    it("persists the access filter under the workspace-namespaced key", async () => {
      setCurrentWorkspace("acme", "ws_a");
      await flush();
      useAgentsViewStore.getState().toggleFilter("access", "specific-people");
      await flush();

      const raw = localStorage.getItem("multica_agents_view:acme");
      expect(raw).not.toBeNull();
      const parsed = JSON.parse(raw as string);
      expect(parsed.state.filters.access).toEqual(["specific-people"]);
    });

    it("rehydrates a saved access filter on workspace switch", async () => {
      localStorage.setItem(
        "multica_agents_view:acme",
        JSON.stringify({
          state: { filters: { access: ["owner-only"] } },
          version: 0,
        }),
      );
      localStorage.setItem(
        "multica_agents_view:beta",
        JSON.stringify({
          state: { filters: { access: ["workspace"] } },
          version: 0,
        }),
      );

      setCurrentWorkspace("acme", "ws_a");
      await flush();
      await flush();
      expect(useAgentsViewStore.getState().filters.access).toEqual(["owner-only"]);

      setCurrentWorkspace("beta", "ws_b");
      await flush();
      await flush();
      expect(useAgentsViewStore.getState().filters.access).toEqual(["workspace"]);
    });

    it("backfills access to [] when rehydrating a pre-access payload", async () => {
      // Pre-access payloads would leave filters.access undefined and crash
      // the row-filter predicate (`filters.access.length`).
      localStorage.setItem(
        "multica_agents_view:acme",
        JSON.stringify({
          state: { filters: { availability: ["online"] } },
          version: 0,
        }),
      );

      setCurrentWorkspace("acme", "ws_a");
      await flush();
      await flush();

      expect(useAgentsViewStore.getState().filters.access).toEqual([]);
    });
  });
});
