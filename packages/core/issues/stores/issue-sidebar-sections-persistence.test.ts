// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { useIssueSidebarSectionsStore } from "@multica/core/issues/stores";

const STORAGE_KEY = "multica_issue_sidebar_sections";
// A save from before the sidebar became cards, when sections could be reordered.
const OLD_ORDER = [
  "execution_log", "properties", "quick_actions", "wakeups", "plugins", "parent", "sub_issues", "duplicates",
  "pull_requests", "environments", "deliverables", "apps", "details", "metadata",
];

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

  it("keeps the sections an older save hid and drops its order", async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      state: { order: OLD_ORDER, hidden: ["parent", "apps", "properties", "apps", "unknown"] },
      version: 0,
    }));
    await useIssueSidebarSectionsStore.persist.rehydrate();
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual(["parent", "apps"]);

    useIssueSidebarSectionsStore.getState().setHidden("metadata", true);
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY)!).state).toEqual({ hidden: ["parent", "apps", "metadata"] });
  });

  it("persists hiding a section, then restores defaults", async () => {
    const store = useIssueSidebarSectionsStore;
    store.getState().setHidden("sub_issues", true);
    await reloadPreferences();
    expect(store.getState().hidden).toEqual(["sub_issues"]);
    store.getState().reset();
    await reloadPreferences();
    expect(store.getState().hidden).toEqual([]);
  });
});
