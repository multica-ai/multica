import { describe, it, expect } from "vitest";
import { getGravatarUrl, resolveAvatarUrl } from "./index";

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

describe("resolveAvatarUrl", () => {
  it("returns null when no avatar URL and gravatar disabled", () => {
    expect(resolveAvatarUrl({ email: "test@example.com", gravatarEnabled: false })).toBeNull();
  });

  it("returns null when no avatar URL, no email, gravatar enabled", () => {
    expect(resolveAvatarUrl({ gravatarEnabled: true })).toBeNull();
    expect(resolveAvatarUrl({ email: null, gravatarEnabled: true })).toBeNull();
    expect(resolveAvatarUrl({ email: "", gravatarEnabled: true })).toBeNull();
  });

  it("returns Gravatar URL when no custom avatar and gravatar enabled", () => {
    const url = resolveAvatarUrl({ email: "test@example.com", gravatarEnabled: true });
    expect(url).toMatch(/^https:\/\/www\.gravatar\.com\/avatar\//);
  });

  it("returns custom avatar URL when provided", () => {
    const url = resolveAvatarUrl({
      avatarUrl: "https://example.com/avatar.png",
      email: "test@example.com",
      gravatarEnabled: true,
    });
    expect(url).toBe("https://example.com/avatar.png");
  });

  it("prefers custom avatar over Gravatar", () => {
    const url = resolveAvatarUrl({
      avatarUrl: "https://example.com/avatar.png",
      email: "test@example.com",
      gravatarEnabled: true,
    });
    expect(url).not.toContain("gravatar.com");
  });

  it("uses resolvePublicFileUrl when provided", () => {
    const mockResolver = (url: string | null | undefined) =>
      url ? `https://cdn.example.com/${url}` : null;
    const url = resolveAvatarUrl({
      avatarUrl: "avatar.png",
      resolvePublicFileUrl: mockResolver,
    });
    expect(url).toBe("https://cdn.example.com/avatar.png");
  });

  it("falls back to Gravatar when resolvePublicFileUrl returns null", () => {
    const mockResolver = () => null;
    const url = resolveAvatarUrl({
      avatarUrl: "avatar.png",
      email: "test@example.com",
      gravatarEnabled: true,
      resolvePublicFileUrl: mockResolver,
    });
    expect(url).toMatch(/^https:\/\/www\.gravatar\.com\/avatar\//);
  });

  it("respects custom size parameter", () => {
    const url = resolveAvatarUrl({
      email: "test@example.com",
      gravatarEnabled: true,
      size: 120,
    });
    expect(url).toContain("s=120");
  });
});
