import { describe, expect, it } from "vitest";
import { skillDisplayName } from "./display";

describe("skillDisplayName", () => {
  it("returns display_name when it is non-empty", () => {
    expect(skillDisplayName({ name: "review-helper", display_name: "代码审查" })).toBe("代码审查");
  });

  it("falls back to name when display_name is missing", () => {
    expect(skillDisplayName({ name: "review-helper" })).toBe("review-helper");
  });

  it("falls back to name when display_name is blank", () => {
    expect(skillDisplayName({ name: "review-helper", display_name: "   " })).toBe("review-helper");
  });

  it("treats an empty display_name as unset", () => {
    expect(skillDisplayName({ name: "review-helper", display_name: "" })).toBe("review-helper");
  });
});
