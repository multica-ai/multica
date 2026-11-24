// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
  ISSUE_SIDEBAR_SECTIONS,
  selectIssueSidebarOrder,
  useIssueSidebarSectionsStore,
} from "@multica/core/issues/stores";

const STORAGE_KEY = "multica_issue_sidebar_sections";
// An order saved while the sidebar still had Sub-issues and Details sections.
const SAVED_ORDER = [
  "properties", "quick_actions", "wakeups", "plugins", "parent", "sub_issues", "duplicates",
  "pull_requests", "environments", "deliverables", "apps", "execution_log", "details", "metadata",
];
const RETIRED = new Set(["sub_issues", "details"]);

async function reloadPreferences() {
  const saved = localStorage.getItem(STORAGE_KEY)!;
  useIssueSidebarSectionsStore.getState().reset();
  localStorage.setItem(STORAGE_KEY, saved);
  await useIssueSidebarSectionsStore.persist.rehydrate();
}

describe("issue sidebar persisted preferences", () => {
  beforeEach(() => {
    localStorage.clear();
    useIssueSidebarSectionsStore.getState().reset();
  });

  it.each([
    { name: "default", order: SAVED_ORDER },
    { name: "customized", order: [...SAVED_ORDER].reverse() },
  ])("drops retired sections from an older $name save and keeps the rest in order", async ({ order: savedOrder }) => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      state: { order: [...savedOrder, "unknown", "parent"], hidden: ["parent", "details", "sub_issues", "apps", "properties", "unknown"] },
      version: 0,
    }));
    await useIssueSidebarSectionsStore.persist.rehydrate();
    const state = useIssueSidebarSectionsStore.getState();
    const order = [...selectIssueSidebarOrder(state)];
    expect(order.filter((id) => id !== "waiting")).toEqual(savedOrder.filter((id) => !RETIRED.has(id)));
    expect(order.indexOf("waiting")).toBe(order.indexOf("parent") + 1);
    expect(state.hidden).toEqual(["parent", "apps"]);
    await reloadPreferences();
    expect(selectIssueSidebarOrder(useIssueSidebarSectionsStore.getState())).toEqual(order);
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual(["parent", "apps"]);
  });

  it("persists hiding and reordering, then restores defaults", async () => {
    const store = useIssueSidebarSectionsStore;
    store.getState().setHidden("duplicates", true);
    store.getState().setOrder(["duplicates", ...ISSUE_SIDEBAR_SECTIONS]);
    await reloadPreferences();
    expect(selectIssueSidebarOrder(store.getState())[0]).toBe("duplicates");
    expect(store.getState().hidden).toEqual(["duplicates"]);
    store.getState().setHidden("duplicates", false);
    await reloadPreferences();
    expect(store.getState().hidden).toEqual([]);
    store.getState().reset();
    await reloadPreferences();
    expect(store.getState().order).toBeNull();
    expect(store.getState().hidden).toEqual([]);
    expect(selectIssueSidebarOrder(store.getState())).toEqual(ISSUE_SIDEBAR_SECTIONS);
  });
});
