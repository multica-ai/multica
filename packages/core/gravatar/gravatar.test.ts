import { describe, it, expect } from "vitest";
import { getGravatarUrl } from "./index";

describe("getGravatarUrl", () => {
  it("returns null for empty or falsy emails", () => {
    expect(getGravatarUrl(null)).toBeNull();
    expect(getGravatarUrl(undefined)).toBeNull();
    expect(getGravatarUrl("")).toBeNull();
    expect(getGravatarUrl("   ")).toBeNull();
  });

  it("returns a Gravatar URL with SHA-256 hash", () => {
    const url = getGravatarUrl("test@example.com");
    expect(url).toMatch(/^https:\/\/www\.gravatar\.com\/avatar\/[a-f0-9]{64}\?/);
  });

  it("normalizes email by trimming and lowercasing", () => {
    const url1 = getGravatarUrl("Test@Example.COM");
    const url2 = getGravatarUrl("  test@example.com  ");
    const url3 = getGravatarUrl("test@example.com");
    expect(url1).toBe(url2);
    expect(url2).toBe(url3);
  });

  it("includes size parameter", () => {
    const url = getGravatarUrl("test@example.com", 120);
    expect(url).toContain("s=120");
  });

  it("includes identicon default and g rating", () => {
    const url = getGravatarUrl("test@example.com");
    expect(url).toContain("d=identicon");
    expect(url).toContain("r=g");
  });

  it("produces correct hash for known vector", () => {
    // SHA-256 of "test@example.com" (already lowercase, trimmed)
    const url = getGravatarUrl("test@example.com");
    // SHA-256("test@example.com") = 973dfe463ec85785f5f95af5ba3906eedb2d931c24e69824a89ea65dba4e813b
    expect(url).toContain("973dfe463ec85785f5f95af5ba3906eedb2d931c24e69824a89ea65dba4e813b");
  });
});
