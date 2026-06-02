import { describe, it, expect } from "vitest";
import { projectUpdateKeys } from "./update-queries";
import { projectKeys } from "./queries";

describe("projectUpdateKeys", () => {
  it("nests updates under the project detail key", () => {
    expect(projectUpdateKeys.list("ws1", "p1")).toEqual([
      ...projectKeys.detail("ws1", "p1"),
      "updates",
    ]);
  });
  it("changes key when workspace changes (workspace isolation)", () => {
    expect(projectUpdateKeys.list("wsA", "p1")).not.toEqual(
      projectUpdateKeys.list("wsB", "p1"),
    );
  });
});
