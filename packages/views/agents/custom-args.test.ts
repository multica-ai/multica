import { describe, expect, it } from "vitest";
import { splitCustomArgEntry } from "./custom-args";

describe("splitCustomArgEntry", () => {
  it("splits the same whitespace-delimited CLI input used by Custom Args rows", () => {
    expect(splitCustomArgEntry(" --max-turns   100\t--verbose\n")).toEqual([
      "--max-turns",
      "100",
      "--verbose",
    ]);
  });

  it("drops empty entries", () => {
    expect(splitCustomArgEntry(" \t\n ")).toEqual([]);
  });
});
