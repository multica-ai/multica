import { describe, expect, it } from "vitest";
import { isUrlUnavailable, sanitizeUrl } from "../src/sanitize-url";

describe("sanitizeUrl", () => {
  it("drops query, hash, and credentials, keeping only origin + pathname", () => {
    const out = sanitizeUrl("https://user:pass@api.example.com/api/issues/123?token=SECRET#frag");
    expect(out).toEqual({ origin: "https://api.example.com", pathname: "/api/issues/123" });
  });

  it("never leaks a secret query value", () => {
    const out = sanitizeUrl("https://x.test/y?access_token=abc123&sig=deadbeef");
    const serialized = JSON.stringify(out);
    expect(serialized).not.toContain("abc123");
    expect(serialized).not.toContain("deadbeef");
    expect(serialized).not.toContain("access_token");
  });

  it("returns nulls (not the raw string) on a parse failure", () => {
    const out = sanitizeUrl("::::not a url::::");
    expect(out).toEqual({ origin: null, pathname: null });
    expect(isUrlUnavailable(out)).toBe(true);
  });

  it("resolves relative resource URLs against a base without keeping the query", () => {
    const out = sanitizeUrl("/api/data?x=secret", "https://app.test/page");
    expect(out).toEqual({ origin: "https://app.test", pathname: "/api/data" });
  });

  it("treats empty input as unavailable", () => {
    expect(sanitizeUrl("")).toEqual({ origin: null, pathname: null });
    expect(sanitizeUrl(undefined)).toEqual({ origin: null, pathname: null });
  });
});
