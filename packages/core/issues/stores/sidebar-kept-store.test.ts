// @vitest-environment node
import { beforeEach, describe, expect, it } from "vitest";
import { useSidebarKeptStore } from "./sidebar-kept-store";

beforeEach(() => {
  useSidebarKeptStore.setState({ held: {} });
});

describe("useSidebarKeptStore", () => {
  it("records the open item's place per list", () => {
    const { hold } = useSidebarKeptStore.getState();
    hold("project:p1", "issue-1", 2);
    hold("builtin:all_issues", "issue-1", 0);
    hold("project:p1", "issue-1", 3);

    expect(useSidebarKeptStore.getState().held).toEqual({
      "project:p1": { id: "issue-1", index: 3 },
      "builtin:all_issues": { id: "issue-1", index: 0 },
    });
  });

  it("keeps the state untouched when nothing changes", () => {
    useSidebarKeptStore.getState().hold("project:p1", "issue-1", 2);
    const before = useSidebarKeptStore.getState().held;
    useSidebarKeptStore.getState().hold("project:p1", "issue-1", 2);
    useSidebarKeptStore.getState().release(["issue-1"]);

    expect(useSidebarKeptStore.getState().held).toBe(before);
  });

  it("forgets places held for items no longer open", () => {
    const { hold, release } = useSidebarKeptStore.getState();
    hold("project:p1", "issue-1", 2);
    hold("projects", "project-1", 0);
    release(["project-1"]);

    expect(useSidebarKeptStore.getState().held).toEqual({ projects: { id: "project-1", index: 0 } });
  });
});
