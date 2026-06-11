import { describe, expect, it } from "vitest";
import {
  getWorkspacePageTitle,
  navigationGroups,
  workspaceFooterNav,
} from "./navigation";

describe("workspace navigation metadata", () => {
  it("keeps the group order and consolidated time entry", () => {
    expect(navigationGroups.map((group) => group.label)).toEqual([
      "Focus",
      "Planning",
      "Tools",
      "Workspace",
    ]);

    expect(navigationGroups[0]?.items.map((item) => item.label)).toEqual([
      "Inbox",
      "My Work",
      "Issues",
      "Archived",
    ]);

    expect(navigationGroups[2]?.items.map((item) => item.label)).toEqual(["Focus"]);

    const flattenedLabels = navigationGroups.flatMap((group) =>
      group.items.map((item) => item.label),
    );

    expect(flattenedLabels).not.toContain("Notifications");
    expect(flattenedLabels).not.toContain("Track time");
    expect(flattenedLabels).not.toContain("My Time");
    expect(flattenedLabels).not.toContain("Team Time");
  });

  it("maps page titles from workspace routes", () => {
    expect(getWorkspacePageTitle("/")).toBe("Inbox");
    expect(getWorkspacePageTitle("/notifications")).toBe("Inbox");
    expect(getWorkspacePageTitle("/issues/issue-123")).toBe("Issues");
    expect(getWorkspacePageTitle("/issues/archived")).toBe("Archived");
    expect(getWorkspacePageTitle("/projects/project-1")).toBe("Projects");
    expect(getWorkspacePageTitle("/focus")).toBe("Focus");
    expect(getWorkspacePageTitle("/pomodoro")).toBe("Focus");
    expect(getWorkspacePageTitle("/settings")).toBe("Settings");
  });

  it("exposes footer labels", () => {
    expect(workspaceFooterNav.map((item) => item.label)).toEqual(["Settings", "Log out"]);
  });
});
