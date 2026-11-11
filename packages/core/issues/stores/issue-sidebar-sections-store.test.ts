import { beforeEach, describe, expect, it } from "vitest";
import {
  ISSUE_SIDEBAR_SECTIONS,
  resolveIssueSidebarHidden,
  resolveIssueSidebarOrder,
  selectIssueSidebarCustomized,
  selectIssueSidebarOrder,
  useIssueSidebarSectionsStore,
} from "./issue-sidebar-sections-store";

describe("resolveIssueSidebarOrder", () => {
  it("falls back to the default order", () => {
    expect(resolveIssueSidebarOrder(null)).toEqual([...ISSUE_SIDEBAR_SECTIONS]);
  });

  it("drops unknown and repeated ids", () => {
    const order = resolveIssueSidebarOrder(["execution_log", "nope", "execution_log", 3, ...ISSUE_SIDEBAR_SECTIONS]);
    expect(order[0]).toBe("execution_log");
    expect(order).toHaveLength(ISSUE_SIDEBAR_SECTIONS.length);
    expect(new Set(order).size).toBe(order.length);
  });

  it("puts a section the save predates after the one it follows by default", () => {
    const saved = ISSUE_SIDEBAR_SECTIONS.filter((id) => id !== "plugins").reverse();
    const order = resolveIssueSidebarOrder(saved);
    expect(order.indexOf("plugins")).toBe(order.indexOf("wakeups") + 1);
  });
});

describe("resolveIssueSidebarHidden", () => {
  it("keeps known sections once and never hides properties", () => {
    expect(resolveIssueSidebarHidden(["details", "details", "properties", "nope"])).toEqual(["details"]);
  });
});

describe("useIssueSidebarSectionsStore", () => {
  beforeEach(() => useIssueSidebarSectionsStore.getState().reset());

  it("stores no order while it matches the default", () => {
    const { setOrder } = useIssueSidebarSectionsStore.getState();
    setOrder(["execution_log", ...ISSUE_SIDEBAR_SECTIONS]);
    expect(selectIssueSidebarOrder(useIssueSidebarSectionsStore.getState())[0]).toBe("execution_log");

    setOrder(ISSUE_SIDEBAR_SECTIONS);
    expect(useIssueSidebarSectionsStore.getState().order).toBeNull();
  });

  it("hides and shows sections", () => {
    const { setHidden } = useIssueSidebarSectionsStore.getState();
    setHidden("details", true);
    setHidden("metadata", true);
    setHidden("properties", true);
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual(["details", "metadata"]);

    setHidden("details", false);
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual(["metadata"]);
  });

  it("resets order and visibility together", () => {
    const state = useIssueSidebarSectionsStore.getState();
    state.setOrder(["metadata"]);
    state.setHidden("apps", true);
    expect(selectIssueSidebarCustomized(useIssueSidebarSectionsStore.getState())).toBe(true);

    useIssueSidebarSectionsStore.getState().reset();
    expect(selectIssueSidebarCustomized(useIssueSidebarSectionsStore.getState())).toBe(false);
  });

  it("sanitizes what it reads back from storage", () => {
    const { merge } = useIssueSidebarSectionsStore.persist.getOptions();
    const state = merge!(
      { order: ["metadata", "bogus", ...ISSUE_SIDEBAR_SECTIONS], hidden: ["properties", "apps"] },
      useIssueSidebarSectionsStore.getState(),
    );
    expect(state.order?.[0]).toBe("metadata");
    expect(state.order).toHaveLength(ISSUE_SIDEBAR_SECTIONS.length);
    expect(state.hidden).toEqual(["apps"]);
  });
});
