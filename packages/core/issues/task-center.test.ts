// @vitest-environment node

import { describe, expect, it } from "vitest";
import { TASK_CENTER_TABS, taskCenterLegacyRedirect, taskCenterPath, taskCenterTabFromSearch } from "./task-center";

describe("Task Center paths", () => {
  it("keeps Tasks experiences inside the existing Issues route without a duplicate Inbox", () => {
    expect(TASK_CENTER_TABS).toEqual(["tasks", "projects", "mine", "automations"]);
    expect(taskCenterTabFromSearch(new URLSearchParams())).toBe("tasks");
    expect(taskCenterTabFromSearch(new URLSearchParams("tab=mine"))).toBe("mine");
    expect(taskCenterTabFromSearch(new URLSearchParams("tab=projects"))).toBe(
      "projects",
    );
    expect(taskCenterTabFromSearch(new URLSearchParams("tab=unknown"))).toBe("tasks");

    expect(taskCenterPath("studio", "projects")).toBe(
      "/studio/issues?tab=projects",
    );
  });

  it("redirects the retired Tasks Activity URL to the canonical standalone Inbox", () => {
    expect(taskCenterLegacyRedirect("studio", new URLSearchParams("tab=activity&issue=task-1"))).toBe("/studio/inbox?issue=task-1");
    expect(taskCenterLegacyRedirect("studio", new URLSearchParams("tab=tasks"))).toBeNull();
  });
});
