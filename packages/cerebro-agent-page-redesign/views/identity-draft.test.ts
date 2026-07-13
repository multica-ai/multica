import { describe, expect, it } from "vitest";
import { descriptionUpdate, nameUpdate } from "./identity-draft";

describe("nameUpdate", () => {
  it("trims a valid name", () => {
    expect(nameUpdate("  Lone  ")).toEqual({ name: "Lone" });
  });

  it("rejects an empty name", () => {
    expect(nameUpdate("   ")).toBeNull();
  });
});

describe("descriptionUpdate", () => {
  it("preserves the entered description", () => {
    expect(descriptionUpdate("  Commercial builder  ")).toEqual({
      description: "  Commercial builder  ",
    });
  });
});
