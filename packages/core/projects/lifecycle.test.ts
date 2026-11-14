// @vitest-environment node
import { describe, expect, it } from "vitest";
import { matchesProjectLifecycle, PROJECT_LIFECYCLE_VIEWS } from "./lifecycle";
import type { ProjectStatus } from "../types";

describe("project lifecycle projections", () => {
  it.each([
    ["planned", [true, false, true]],
    ["in_progress", [true, false, true]],
    ["paused", [true, false, true]],
    ["completed", [false, true, true]],
    ["cancelled", [false, true, true]],
  ] as [ProjectStatus, boolean[]][])("projects %s consistently across clients", (status, expected) => {
    expect(PROJECT_LIFECYCLE_VIEWS.map((view) => matchesProjectLifecycle(status, view))).toEqual(expected);
  });
});
