// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({ shell: {} }));

import { normalizeUrl } from "./issue-browser-url";

describe("normalizeUrl", () => {
  it("keeps http(s) URLs", () => {
    expect(normalizeUrl("https://example.com/a")).toBe("https://example.com/a");
  });
  it("adds a scheme to bare hosts", () => {
    expect(normalizeUrl("example.com")).toBe("https://example.com");
    expect(normalizeUrl("localhost:3000")).toBe("http://localhost:3000");
  });
  it("treats dotted host with port as a host", () => {
    expect(normalizeUrl("example.com:8080")).toBe("http://example.com:8080");
    expect(normalizeUrl("example.com:8080/path")).toBe("http://example.com:8080/path");
  });
  it("searches for free text", () => {
    expect(normalizeUrl("hello world")).toBe("https://www.google.com/search?q=hello%20world");
  });
  it("rejects non-http schemes", () => {
    expect(normalizeUrl("javascript:alert(1)")).toBeNull();
    expect(normalizeUrl("file:///etc/passwd")).toBeNull();
  });
  it("rejects empty input", () => {
    expect(normalizeUrl("  ")).toBeNull();
  });
});
