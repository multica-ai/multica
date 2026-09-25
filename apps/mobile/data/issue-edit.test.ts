import { describe, expect, it } from "vitest";

import { buildIssueTextUpdate } from "./issue-edit";

describe("buildIssueTextUpdate", () => {
  it("keeps mobile text saves last-write-wins until conflict reconciliation exists", () => {
    expect(buildIssueTextUpdate("  Updated title  ", "  Updated body  ")).toEqual({
      title: "Updated title",
      description: "Updated body",
    });
  });
});
