import { describe, it, expect } from "vitest";
import { countMatches, replaceAll, replaceFirst } from "./document-find-replace";

describe("countMatches", () => {
  it("counts case-insensitive occurrences", () => {
    expect(countMatches("Tal og tal og TAL", "tal")).toBe(3);
  });
  it("returns 0 for an empty query", () => {
    expect(countMatches("anything", "")).toBe(0);
  });
  it("treats the query as literal text, not a regex", () => {
    expect(countMatches("a.b a.b axb", "a.b")).toBe(2);
  });
});

describe("replaceAll", () => {
  it("replaces every occurrence and reports the count", () => {
    expect(replaceAll("EBITDA og ebitda", "ebitda", "DB")).toEqual({
      body: "DB og DB",
      count: 2,
    });
  });
  it("inserts the replacement literally ($ is not a backreference)", () => {
    expect(replaceAll("price", "price", "$5").body).toBe("$5");
  });
  it("is a no-op for an empty query", () => {
    expect(replaceAll("keep me", "", "x")).toEqual({ body: "keep me", count: 0 });
  });
});

describe("replaceFirst", () => {
  it("replaces only the first occurrence", () => {
    expect(replaceFirst("tal tal tal", "tal", "X")).toEqual({
      body: "X tal tal",
      replaced: true,
    });
  });
  it("reports replaced=false when nothing matches", () => {
    expect(replaceFirst("abc", "z", "X")).toEqual({ body: "abc", replaced: false });
  });
});
