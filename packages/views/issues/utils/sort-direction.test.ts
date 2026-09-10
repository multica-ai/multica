// @vitest-environment node
import { describe, expect, it } from "vitest";
import { sortDirectionLabelKey } from "./sort-direction";

describe("sortDirectionLabelKey", () => {
  it("describes date and priority order in user terms", () => {
    expect(sortDirectionLabelKey("created_at", "desc")).toBe("newest_first");
    expect(sortDirectionLabelKey("due_date", "asc")).toBe("earliest_first");
    expect(sortDirectionLabelKey("priority", "asc")).toBe(
      "highest_priority_first",
    );
  });

  it("falls back to generic direction for a custom property", () => {
    expect(sortDirectionLabelKey("property:score", "desc")).toBe(
      "descending_title",
    );
  });
});
