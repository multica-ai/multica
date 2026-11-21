// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
  ISSUE_SIDEBAR_SECTIONS,
  selectIssueSidebarOrder,
  useIssueSidebarSectionsStore,
} from "@multica/core/issues/stores";

const STORAGE_KEY = "multica_issue_sidebar_sections";
// The order persisted before the Sub-issues section was introduced.
const OLD_ORDER = [
  "properties", "quick_actions", "wakeups", "plugins", "parent", "duplicates",
  "pull_requests", "environments", "deliverables", "apps", "execution_log", "details", "metadata",
];

async function reloadPreferences() {
  const saved = localStorage.getItem(STORAGE_KEY)!;
  useIssueSidebarSectionsStore.getState().reset();
  localStorage.setItem(STORAGE_KEY, saved);
  await useIssueSidebarSectionsStore.persist.rehydrate();
}

describe("Sub-issues sidebar persisted preferences", () => {
  beforeEach(() => {
    localStorage.clear();
    useIssueSidebarSectionsStore.getState().reset();
  });

  it.each([
    { name: "default", order: OLD_ORDER },
    { name: "customized", order: [...OLD_ORDER].reverse() },
  ])("hydrates an older $name order without changing existing relative order", async ({ order: savedOrder }) => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      state: { order: [...savedOrder, "unknown", "parent"], hidden: ["parent", "apps", "properties", "apps", "unknown"] },
      version: 0,
    }));
    await useIssueSidebarSectionsStore.persist.rehydrate();
    const state = useIssueSidebarSectionsStore.getState();
    const order = [...selectIssueSidebarOrder(state)];
    expect(order.filter((id) => id !== "sub_issues")).toEqual(savedOrder);
    expect(order.indexOf("sub_issues")).toBe(order.indexOf("parent") + 1);
    expect(order.filter((id) => id === "sub_issues")).toHaveLength(1);
    expect(state.hidden).toEqual(["parent", "apps"]);
    await reloadPreferences();
    expect(selectIssueSidebarOrder(useIssueSidebarSectionsStore.getState())).toEqual(order);
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual(["parent", "apps"]);
  });

  it("persists hiding and reordering Sub-issues, then restores defaults", async () => {
    const store = useIssueSidebarSectionsStore;
    store.getState().setHidden("sub_issues", true);
    store.getState().setOrder(["sub_issues", ...ISSUE_SIDEBAR_SECTIONS]);
    await reloadPreferences();
    expect(selectIssueSidebarOrder(store.getState())[0]).toBe("sub_issues");
    expect(store.getState().hidden).toEqual(["sub_issues"]);
    store.getState().setHidden("sub_issues", false);
    await reloadPreferences();
    expect(store.getState().hidden).toEqual([]);
    expect(selectIssueSidebarOrder(store.getState())[0]).toBe("sub_issues");
    store.getState().reset();
    await reloadPreferences();
    expect(store.getState().order).toBeNull();
    expect(store.getState().hidden).toEqual([]);
    expect(selectIssueSidebarOrder(store.getState())).toEqual(ISSUE_SIDEBAR_SECTIONS);
  });

  it("shows a newly added section for an uncustomized older save", async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ state: { order: null, hidden: ["metadata"] }, version: 0 }));
    await useIssueSidebarSectionsStore.persist.rehydrate();
    expect(selectIssueSidebarOrder(useIssueSidebarSectionsStore.getState())).toContain("sub_issues");
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual(["metadata"]);
  });
});
