// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildSurfaceFrameDocument } from "./surface-document";

describe("plugin surface frame document", () => {
  it("allows the nested frame to navigate only to the hosted content origin", () => {
    const document = buildSurfaceFrameDocument({
      url: "https://plugin-content.example.test/plugin-surfaces/opaque-token",
      bridgeToken: "one-use-proof",
    });

    expect(document).toContain("frame-src https://plugin-content.example.test");
    expect(document).not.toContain("frame-src https:;");
    expect(document).toContain("connect-src 'none'");
  });

  it("keeps the untrusted inner frame opaque", () => {
    const document = buildSurfaceFrameDocument({
      url: "https://plugin-content.example.test/plugin-surfaces/opaque-token",
      bridgeToken: "one-use-proof",
    });

    expect(document).toContain('child.setAttribute("sandbox", "allow-scripts")');
    expect(document).not.toContain('child.setAttribute("sandbox", "allow-scripts allow-same-origin")');
  });

  it("checks source, protocol, challenge and one-time use before relaying a guest port", () => {
    const document = buildSurfaceFrameDocument({
      url: "https://plugin-content.example.test/plugin-surfaces/opaque-token",
      bridgeToken: "one-use-proof",
    });

    expect(document).toContain("event.source !== child.contentWindow");
    expect(document).toContain("data.version !== 2");
    expect(document).toContain("data.challenge !== config.bridgeToken");
    expect(document).toContain('state !== "launching"');
    expect(document).toContain('state = "connected"');
  });

  it("uses terminal state instead of guessing when CSP reports navigation", () => {
    const document = buildSurfaceFrameDocument({
      url: "https://plugin-content.example.test/plugin-surfaces/opaque-token",
      bridgeToken: "one-use-proof",
    });

    expect(document).toContain("securitypolicyviolation");
    expect(document).toContain("multica:plugin-surface-navigation-blocked");
    expect(document).toContain('if (state === "terminal") return');
    expect(document).not.toContain("setTimeout(");
  });

  it("never interpolates the launch URL or proof into executable source", () => {
    const document = buildSurfaceFrameDocument({
      url: "https://plugin-content.example.test/plugin-surfaces/%3C/script%3E",
      bridgeToken: `proof</script><script>alert(1)</script>`,
    });

    expect(document).not.toContain("alert(1)");
    expect(document).not.toContain("proof</script>");
  });

  it("rejects a non-HTTP launch", () => {
    expect(() => buildSurfaceFrameDocument({ url: "javascript:alert(1)", bridgeToken: "proof" })).toThrow();
  });
});
