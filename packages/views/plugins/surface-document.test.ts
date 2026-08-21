// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  buildSurfaceCSP,
  buildSurfaceDocument,
  surfaceConnectSources,
} from "./surface-document";

// The document the host generates IS the sandbox policy. These pin the parts a
// reviewer cannot verify by reading the component: what a surface is allowed to
// talk to, and that nothing about the plugin can escape into the markup.

describe("surface connect-src", () => {
  it("derives the allowlist from granted net: scopes only", () => {
    expect(surfaceConnectSources(["issues:read", "net:example.com", "storage:user", "net:api.example.com"]))
      .toEqual(["https://example.com", "https://api.example.com"]);
  });

  it("names no remote origin when no net: scope was granted", () => {
    // While the code loaded from the author's server, script-src had to name
    // that origin, so a surface could always reach its author back. The code is
    // ours to serve now, so this policy names no remote origin of any kind.
    //
    // That bounds what the DOCUMENT can request. It is not the same claim as
    // "a surface cannot reach its author": the sandbox still permits the frame
    // to navigate itself, which no CSP directive covers. See the known gap in
    // surface-document.ts.
    const csp = buildSurfaceCSP(["issues:read"]);
    expect(csp).toContain("connect-src 'none'");
    expect(csp).toContain("script-src 'unsafe-inline'");
    expect(csp).not.toContain("https://");
  });

  it("keeps the cheapest side channels closed", () => {
    // <img> and webfont URLs are the two exfiltration paths that need no
    // scripting at all.
    const csp = buildSurfaceCSP(["net:example.com"]);
    expect(csp).toContain("img-src data: blob:");
    expect(csp).toContain("font-src data:");
  });

  it("allows only the domains that were granted", () => {
    const csp = buildSurfaceCSP(["net:example.com"]);
    expect(csp).toContain("connect-src https://example.com");
    expect(csp).not.toContain("https://cdn.example.com");
  });

  it("denies everything by default and allows no plugin-controlled framing", () => {
    const csp = buildSurfaceCSP([]);
    expect(csp).toContain("default-src 'none'");
    // frame-ancestors is intentionally absent — <meta> ignores it, and the
    // sandbox already denies this document the ability to frame anything.
    expect(csp).not.toContain("frame-ancestors");
    expect(csp).toContain("base-uri 'none'");
    expect(csp).toContain("form-action 'none'");
  });
});

describe("surface document", () => {
  it("puts the policy before anything it governs", () => {
    const document = buildSurfaceDocument({
      code: "console.log('hi');",
      grantedScopes: ["net:example.com"],
      theme: {},
    });
    const cspIndex = document.indexOf("Content-Security-Policy");
    const scriptIndex = document.indexOf("<script");
    expect(cspIndex).toBeGreaterThan(-1);
    expect(cspIndex).toBeLessThan(scriptIndex);
  });

  it("fetches nothing to render a surface", () => {
    // The point of hosting the artifact: rendering the panel issues no request,
    // so a well-behaved plugin's author learns nothing about who opened it.
    const document = buildSurfaceDocument({ code: "console.log('hi');", grantedScopes: [], theme: {} });
    expect(document).not.toContain("<script src=");
  });

  it("does not mistake an ordinary host reload for hostile navigation", () => {
    // pagehide fires for both self-navigation and a host-authored srcDoc reload,
    // so it cannot be used as a security signal. The real Chromium regression
    // test reloads the document; this pins that no beacon is generated at all.
    const document = buildSurfaceDocument({ code: "console.log('hi');", grantedScopes: [], theme: {} });
    expect(document).not.toContain("pagehide");
    expect(document).not.toContain("multica:plugin-surface-navigated");
  });

  it("installs browser error listeners before plugin code runs", () => {
    const document = buildSurfaceDocument({ code: "throw new Error('boom');", grantedScopes: [], theme: {} });
    const executeIndex = document.indexOf("document.body.appendChild(element)");
    expect(document.indexOf('addEventListener("error"')).toBeLessThan(executeIndex);
    expect(document.indexOf('addEventListener("unhandledrejection"')).toBeLessThan(executeIndex);
    expect(document).toContain("multica:plugin-surface-error-ack");
  });

  it("carries code that would otherwise close the script element early", () => {
    // A surface that contains the literal end tag inside a string is ordinary
    // JavaScript. Base64 is what makes it impossible for the HTML tokenizer to
    // react to a plugin's source at all.
    const code = `const html = "</script><script>alert(1)</script>";`;
    const document = buildSurfaceDocument({ code, grantedScopes: [], theme: {} });
    expect(document).not.toContain("alert(1)");
    expect(document).not.toContain('"</script>');

    const encoded = /id="multica-surface-code">([^<]*)</.exec(document)?.[1] ?? "";
    expect(encoded).not.toBe("");
    expect(new TextDecoder().decode(Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0))))
      .toBe(code);
  });

  it("round-trips non-ASCII source", () => {
    // Encoding through latin1 would corrupt this silently, and the surface
    // would fail at runtime with a message pointing nowhere near the cause.
    const code = `const 消息 = "部署已完成 ✅";`;
    const document = buildSurfaceDocument({ code, grantedScopes: [], theme: {} });
    const encoded = /id="multica-surface-code">([^<]*)</.exec(document)?.[1] ?? "";
    expect(new TextDecoder().decode(Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0))))
      .toBe(code);
  });

  it("forwards theme tokens as custom properties so a surface looks native untouched", () => {
    const document = buildSurfaceDocument({
      code: "console.log('hi');",
      grantedScopes: [],
      theme: { "--background": "oklch(1 0 0)", "--radius": "6px" },
    });
    expect(document).toContain("--background: oklch(1 0 0);");
    expect(document).toContain("--radius: 6px;");
  });
});
