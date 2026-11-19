// @vitest-environment node
import { describe, expect, it } from "vitest";
import { joinServerPath, normalizeServerUrl, serverWebSocketUrl } from "./server-url";

describe("server addresses", () => {
  it.each([
    [" HTTPS://EXAMPLE.COM:8443/multica/// ", "https://example.com:8443/multica"],
    ["https://api.example.test/api/", "https://api.example.test"],
    ["https://example.test:8443/proxy/api", "https://example.test:8443/proxy"],
    ["https://[::1]:8443/proxy", "https://[::1]:8443/proxy"],
  ])("normalizes API root %s", (input, expected) => {
    expect(normalizeServerUrl(input, "api")).toBe(expected);
  });

  it("preserves the independently selected web base path", () => {
    expect(normalizeServerUrl("https://web.example.test:9443/ui/api/", "web"))
      .toBe("https://web.example.test:9443/ui/api");
  });

  it.each([
    "", "example.test", "ftp://example.test", "https://", "https://user:pass@example.test",
    "https://example.test?a=1", "https://example.test#frag", "https://example.test:99999",
    "https://example.test/a/../b", "https://example.test/%2e%2e/b",
    "https://example.test/a b", "https://example.test/a\\b", "https://example.test/a%2fb",
  ])("rejects an ambiguous/invalid address %s", (input) => {
    expect(() => normalizeServerUrl(input, "api")).toThrow("invalid");
  });

  it("explains the HTTPS requirement", () => {
    expect(() => normalizeServerUrl("http://10.0.2.2:8080", "api")).toThrow("https_required");
  });

  it.each(["https://same.example.test:8443", "https://api.example.test:9443/proxy"])(
    "keeps auth, API, WS and attachments on %s", (base) => {
      expect(joinServerPath(base, "/auth/send-code")).toBe(`${base}/auth/send-code`);
      expect(joinServerPath(base, "/api/me")).toBe(`${base}/api/me`);
      expect(joinServerPath(base, "/api/attachments/id/download?q=1")).toBe(`${base}/api/attachments/id/download?q=1`);
      expect(serverWebSocketUrl(base)).toBe(`${base.replace("https:", "wss:")}/ws`);
    },
  );

  it("preserves a client route whose first segment is also the proxy prefix", () => {
    expect(joinServerPath("https://host.test:8443/auth", "/auth/send-code"))
      .toBe("https://host.test:8443/auth/auth/send-code");
  });
});
