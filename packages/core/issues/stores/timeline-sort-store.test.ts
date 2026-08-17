// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useTimelineSortStore } from "./timeline-sort-store";

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values. See comment-collapse-store.test.ts for context.
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
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: storage,
    });
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: storage,
    });
  }
});

beforeEach(() => {
  localStorage.removeItem("multica_timeline_sort");
  useTimelineSortStore.setState({ direction: "oldest" });
});

describe("timeline sort store", () => {
  it("defaults to oldest", () => {
    expect(useTimelineSortStore.getState().direction).toBe("oldest");
  });

  it("toggles between oldest and newest", () => {
    useTimelineSortStore.getState().toggle();
    expect(useTimelineSortStore.getState().direction).toBe("newest");
    useTimelineSortStore.getState().toggle();
    expect(useTimelineSortStore.getState().direction).toBe("oldest");
  });

  it("setDirection applies the value", () => {
    useTimelineSortStore.getState().setDirection("newest");
    expect(useTimelineSortStore.getState().direction).toBe("newest");
  });

  it("persists to localStorage", () => {
    useTimelineSortStore.getState().setDirection("newest");
    const raw = localStorage.getItem("multica_timeline_sort");
    expect(raw).toBeTruthy();
    expect(JSON.parse(raw!).state.direction).toBe("newest");
  });

  it("rejects unknown persisted values via merge guard", () => {
    localStorage.setItem(
      "multica_timeline_sort",
      JSON.stringify({ state: { direction: "sideways" }, version: 0 }),
    );
    // Rehydrate by recreating the persisted store: zustand reads on next
    // `persist.rehydrate` call. Easiest is to call the rehydrate API if
    // exposed; otherwise assert the merge behaviour directly through a fresh
    // store by clearing module state is not possible in vitest, so test the
    // guard by re-running the merge through manually setting localStorage and
    // then invoking hasHydrated / rehydrate.
    //
    // zustand persist exposes `persist.rehydrate()` on the store API:
    const api = useTimelineSortStore as unknown as {
      persist: { rehydrate: () => Promise<void> };
    };
    return api.persist.rehydrate().then(() => {
      expect(useTimelineSortStore.getState().direction).toBe("oldest");
    });
  });
});
