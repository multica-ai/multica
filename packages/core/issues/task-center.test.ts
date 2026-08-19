// @vitest-environment node

import { describe, expect, it } from "vitest";
import { taskCenterPath, taskCenterTabFromSearch } from "./task-center";

describe("Task Center paths", () => {
  it("keeps all Tasks experiences inside the existing Issues route", () => {
    expect(taskCenterTabFromSearch(new URLSearchParams())).toBe("tasks");
    expect(taskCenterTabFromSearch(new URLSearchParams("tab=mine"))).toBe("mine");
    expect(taskCenterTabFromSearch(new URLSearchParams("tab=unknown"))).toBe("tasks");

    expect(taskCenterPath("studio", "activity")).toBe(
      "/studio/issues?tab=activity",
    );
  });
});
