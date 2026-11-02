// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_HIDDEN_COLUMNS,
  EMPTY_SKILL_FILTERS,
  useSkillsViewStore,
} from "./view-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

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
  useSkillsViewStore.setState({
    sortField: "updated",
    sortDirection: "desc",
    hiddenColumns: DEFAULT_HIDDEN_COLUMNS,
    filters: EMPTY_SKILL_FILTERS,
  });
  setCurrentWorkspace(null, null);
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useSkillsViewStore", () => {
  it("backfills new filter dimensions when rehydrating a pre-labels payload", async () => {
    // A payload persisted before the `labels` filter existed must not drop
    // the key to undefined (the skills list filter predicate reads
    // `filters.labels.length` and would crash).
    localStorage.setItem(
      "multica_skills_view:acme",
      JSON.stringify({
        state: { filters: { usage: ["used"], origins: [], agents: [], creators: [] } },
        version: 0,
      }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    const filters = useSkillsViewStore.getState().filters;
    expect(filters.labels).toEqual([]);
    expect(filters.usage).toEqual(["used"]);
  });
});
