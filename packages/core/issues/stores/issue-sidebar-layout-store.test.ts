import { beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_ISSUE_SIDEBAR_LAYOUT,
  dropIssueSidebarTab,
  mergeIssueSidebarGroups,
  moveIssueSidebarTab,
  normalizeIssueSidebarLayout,
  resolveIssueSidebarLayout,
  selectIssueSidebarLayout,
  splitIssueSidebarTab,
  useIssueSidebarLayoutStore,
  type IssueSidebarLayout,
} from "./issue-sidebar-layout-store";

const SPLIT: IssueSidebarLayout = [
  { id: "g1", tabs: ["overview", "delivery"], active: "overview" },
  { id: "g2", tabs: ["runs"], active: "runs" },
];

describe("resolveIssueSidebarLayout", () => {
  it("falls back to the default for anything that is not a list", () => {
    expect(resolveIssueSidebarLayout(null)).toEqual(DEFAULT_ISSUE_SIDEBAR_LAYOUT);
    expect(resolveIssueSidebarLayout({ tabs: ["runs"] })).toEqual(DEFAULT_ISSUE_SIDEBAR_LAYOUT);
  });

  it("keeps a saved split layout", () => {
    expect(resolveIssueSidebarLayout(SPLIT)).toEqual(SPLIT);
  });

  it("drops unknown and repeated tabs, empty groups and invalid active tabs", () => {
    expect(
      resolveIssueSidebarLayout([
        { id: "g1", tabs: ["runs", "plugins", "runs"], active: "plugins" },
        { id: "g2", tabs: [] },
        "junk",
        { id: "g3", tabs: ["overview", "delivery"], active: "delivery" },
      ]),
    ).toEqual([
      { id: "g1", tabs: ["runs"], active: "runs" },
      { id: "g3", tabs: ["overview", "delivery"], active: "delivery" },
    ]);
  });

  it("adds a tab missing from the saved layout to the first group", () => {
    expect(resolveIssueSidebarLayout([{ id: "g1", tabs: ["runs"], active: "runs" }, { id: "g2", tabs: ["overview"], active: "overview" }])).toEqual([
      { id: "g1", tabs: ["runs", "delivery"], active: "runs" },
      { id: "g2", tabs: ["overview"], active: "overview" },
    ]);
  });

  it("renames a repeated group id", () => {
    const layout = resolveIssueSidebarLayout([
      { id: "g1", tabs: ["overview"], active: "overview" },
      { id: "g1", tabs: ["delivery", "runs"], active: "runs" },
    ]);
    expect(layout.map((group) => group.id)).toEqual(["g1", "g2"]);
  });
});

describe("moveIssueSidebarTab", () => {
  it("reorders within a group like arrayMove", () => {
    const [group] = moveIssueSidebarTab(DEFAULT_ISSUE_SIDEBAR_LAYOUT, "runs", "g1", 1);
    expect(group!.tabs).toEqual(["overview", "runs", "delivery"]);
    const [back] = moveIssueSidebarTab(DEFAULT_ISSUE_SIDEBAR_LAYOUT, "overview", "g1", 2);
    expect(back!.tabs).toEqual(["delivery", "runs", "overview"]);
  });

  it("leaves an emptied group in place until normalized", () => {
    const moved = moveIssueSidebarTab(SPLIT, "runs", "g1", 1);
    expect(moved).toEqual([
      { id: "g1", tabs: ["overview", "runs", "delivery"], active: "overview" },
      { id: "g2", tabs: [], active: "runs" },
    ]);
    expect(normalizeIssueSidebarLayout(moved)).toEqual([{ id: "g1", tabs: ["overview", "runs", "delivery"], active: "overview" }]);
  });
});

describe("splitIssueSidebarTab", () => {
  it("opens a new group below or above the target", () => {
    expect(splitIssueSidebarTab(DEFAULT_ISSUE_SIDEBAR_LAYOUT, "runs", "g1", "below")).toEqual(SPLIT);
    expect(splitIssueSidebarTab(DEFAULT_ISSUE_SIDEBAR_LAYOUT, "runs", "g1", "above")).toEqual([
      { id: "g2", tabs: ["runs"], active: "runs" },
      { id: "g1", tabs: ["overview", "delivery"], active: "overview" },
    ]);
  });

  it("does nothing for the only tab of the target group", () => {
    expect(splitIssueSidebarTab(SPLIT, "runs", "g2", "above")).toBe(SPLIT);
  });
});

describe("mergeIssueSidebarGroups", () => {
  it("joins every tab into the first group in visible order", () => {
    expect(mergeIssueSidebarGroups(SPLIT)).toEqual([{ id: "g1", tabs: ["overview", "delivery", "runs"], active: "overview" }]);
  });
});

describe("dropIssueSidebarTab", () => {
  it("splits on a zone and shows the dropped tab", () => {
    expect(dropIssueSidebarTab(DEFAULT_ISSUE_SIDEBAR_LAYOUT, "runs", { type: "zone", group: "g1", edge: "below" })).toEqual(SPLIT);
  });

  it("moves onto another group's tab and selects it there", () => {
    expect(dropIssueSidebarTab(SPLIT, "runs", { type: "tab", tab: "delivery" })).toEqual([
      { id: "g1", tabs: ["overview", "runs", "delivery"], active: "runs" },
      { id: "g2", tabs: [], active: "runs" },
    ]);
  });

  it("appends when dropped on another group's empty strip space", () => {
    expect(dropIssueSidebarTab(SPLIT, "delivery", { type: "strip", group: "g2" })).toEqual([
      { id: "g1", tabs: ["overview"], active: "overview" },
      { id: "g2", tabs: ["runs", "delivery"], active: "delivery" },
    ]);
  });

  it("leaves the layout alone when nothing moves", () => {
    expect(dropIssueSidebarTab(SPLIT, "runs", { type: "tab", tab: "runs" })).toBe(SPLIT);
    expect(dropIssueSidebarTab(SPLIT, "runs", { type: "strip", group: "g2" })).toBe(SPLIT);
  });
});

describe("useIssueSidebarLayoutStore", () => {
  beforeEach(() => {
    useIssueSidebarLayoutStore.setState({ layout: null });
  });

  it("saves a changed layout and forgets it once back to the default", () => {
    const { setLayout } = useIssueSidebarLayoutStore.getState();
    setLayout(SPLIT);
    expect(useIssueSidebarLayoutStore.getState().layout).toEqual(SPLIT);
    setLayout(mergeIssueSidebarGroups(SPLIT));
    expect(useIssueSidebarLayoutStore.getState().layout).toBeNull();
  });

  it("selects a tab in whichever group holds it", () => {
    useIssueSidebarLayoutStore.getState().setLayout(SPLIT);
    useIssueSidebarLayoutStore.getState().selectTab("delivery");
    expect(selectIssueSidebarLayout(useIssueSidebarLayoutStore.getState())[0]!.active).toBe("delivery");
  });

  it("drops empty groups when a layout is committed", () => {
    useIssueSidebarLayoutStore.getState().setLayout(moveIssueSidebarTab(SPLIT, "runs", "g1", 2));
    expect(useIssueSidebarLayoutStore.getState().layout).toBeNull();
  });
});
