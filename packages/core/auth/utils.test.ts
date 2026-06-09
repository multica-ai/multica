import { describe, expect, it } from "vitest";
import { sanitizeNextUrl } from "./utils";

describe("sanitizeNextUrl", () => {
  it("accepts single-slash relative paths", () => {
    expect(sanitizeNextUrl("/issues")).toBe("/issues");
    expect(sanitizeNextUrl("/invite/123")).toBe("/invite/123");
    expect(sanitizeNextUrl("/issues?tab=assigned#top")).toBe(
      "/issues?tab=assigned#top",
    );
  });

  it("returns null for null or empty input", () => {
    expect(sanitizeNextUrl(null)).toBeNull();
    expect(sanitizeNextUrl("")).toBeNull();
  });

  it("rejects absolute URLs", () => {
    expect(sanitizeNextUrl("https://evil.example")).toBeNull();
    expect(sanitizeNextUrl("http://evil.example/path")).toBeNull();
  });

  it("rejects javascript: and other non-http schemes", () => {
    // Caught by the leading-slash rule, but named here so future edits
    // to the regex don't silently drop protection against this vector.
    expect(sanitizeNextUrl("javascript:alert(1)")).toBeNull();
    expect(sanitizeNextUrl("data:text/html,<script>")).toBeNull();
  });

  it("rejects protocol-relative URLs", () => {
    expect(sanitizeNextUrl("//evil.example")).toBeNull();
    expect(sanitizeNextUrl("//evil.example/path")).toBeNull();
  });

  it("rejects paths containing backslashes", () => {
    expect(sanitizeNextUrl("/\\evil.example")).toBeNull();
    expect(sanitizeNextUrl("\\\\evil.example")).toBeNull();
  });

  it("rejects paths containing control characters", () => {
    expect(sanitizeNextUrl("/safe\u0000bad")).toBeNull();
    expect(sanitizeNextUrl("/safe\tbad")).toBeNull();
    expect(sanitizeNextUrl("/safe\r\nbad")).toBeNull();
  });
});
