// @vitest-environment node

import { describe, expect, it } from "vitest";
import { remapTaskCenterPath, taskCenterTabFromSearch } from "./task-center";

describe("Task Center paths", () => {
  it("keeps all Tasks experiences inside the existing Issues route", () => {
    expect(taskCenterTabFromSearch(new URLSearchParams())).toBe("tasks");
    expect(taskCenterTabFromSearch(new URLSearchParams("tab=mine"))).toBe("mine");
    expect(taskCenterTabFromSearch(new URLSearchParams("tab=unknown"))).toBe("tasks");

    expect(remapTaskCenterPath("/studio/inbox")).toBe("/studio/issues?tab=activity");
    expect(remapTaskCenterPath("/studio/autopilots")).toBe("/studio/issues?tab=automations");
    expect(remapTaskCenterPath("/studio/my-issues")).toBe("/studio/issues?tab=mine");
  });

  it("preserves Task detail links and unrelated paths", () => {
    expect(remapTaskCenterPath("/studio/issues/MUL-7")).toBe("/studio/issues/MUL-7");
    expect(remapTaskCenterPath("/studio/chat?agent=a1")).toBe("/studio/chat?agent=a1");
  });
});
