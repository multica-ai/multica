import { beforeEach, describe, expect, it } from "vitest";
import {
  ISSUE_SIDEBAR_GROUPS,
  ISSUE_SIDEBAR_SECTIONS,
  resolveIssueSidebarHidden,
  selectIssueSidebarCustomized,
  useIssueSidebarSectionsStore,
} from "./issue-sidebar-sections-store";

describe("ISSUE_SIDEBAR_SECTIONS", () => {
  it("lists every grouped section once, in card order", () => {
    expect(ISSUE_SIDEBAR_SECTIONS).toEqual(ISSUE_SIDEBAR_GROUPS.flat());
    expect(new Set(ISSUE_SIDEBAR_SECTIONS).size).toBe(ISSUE_SIDEBAR_SECTIONS.length);
  });
});

describe("resolveIssueSidebarHidden", () => {
  it("keeps known sections once and never hides properties", () => {
    expect(resolveIssueSidebarHidden(["details", "details", "properties", "nope"])).toEqual(["details"]);
  });
});

describe("useIssueSidebarSectionsStore", () => {
  beforeEach(() => useIssueSidebarSectionsStore.getState().reset());

  it("hides and shows sections", () => {
    const { setHidden } = useIssueSidebarSectionsStore.getState();
    setHidden("details", true);
    setHidden("metadata", true);
    setHidden("properties", true);
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual(["details", "metadata"]);

    setHidden("details", false);
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual(["metadata"]);
  });

  it("resets visibility", () => {
    useIssueSidebarSectionsStore.getState().setHidden("apps", true);
    expect(selectIssueSidebarCustomized(useIssueSidebarSectionsStore.getState())).toBe(true);

    useIssueSidebarSectionsStore.getState().reset();
    expect(selectIssueSidebarCustomized(useIssueSidebarSectionsStore.getState())).toBe(false);
  });

  it("sanitizes what it reads back from storage and drops a saved order", () => {
    const { merge } = useIssueSidebarSectionsStore.persist.getOptions();
    const state = merge!(
      { order: ["metadata", "bogus", ...ISSUE_SIDEBAR_SECTIONS], hidden: ["properties", "apps"] },
      useIssueSidebarSectionsStore.getState(),
    );
    expect(state.hidden).toEqual(["apps"]);
    expect(state).not.toHaveProperty("order");
  });
});
