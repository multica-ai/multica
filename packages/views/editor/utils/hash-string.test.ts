// @vitest-environment node
import { describe, expect, it } from "vitest";
import { hashString } from "./hash-string";

describe("hashString", () => {
  it("returns a stable base36 digest for identical input", () => {
    expect(hashString("hello world")).toBe(hashString("hello world"));
  });

  it("differs for different inputs (collision-resistant enough for contentKey)", () => {
    expect(hashString("<p>a</p>")).not.toBe(hashString("<p>b</p>"));
  });

  it("produces [a-z0-9]+ tokens (safe to inline as a JS string without escaping)", () => {
    const token = hashString("<h1>标题 😀</h1>\n<script>alert(1)</script>");
    expect(token).toMatch(/^[a-z0-9]+$/);
  });

  it("hashes empty string without throwing", () => {
    expect(hashString("")).toMatch(/^[a-z0-9]+$/);
  });
});
