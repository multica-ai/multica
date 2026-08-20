// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { PluginInstallation, PluginSurface } from "@multica/core/types";
import {
  buildSurfaceCSP,
  buildSurfaceDocument,
  resolveSurfaceEntry,
  surfaceConnectSources,
} from "./surface-document";

// The document the host generates IS the sandbox policy. These pin the parts a
// reviewer cannot verify by reading the component: what a surface is allowed to
// talk to, and that nothing about the plugin can escape into the markup.

const SURFACE: PluginSurface = {
  key: "hello",
  type: "issue_panel",
  name: "Hello",
  entry: "ui/main.js",
  platforms: [],
};

function installation(overrides: Partial<PluginInstallation> = {}): PluginInstallation {
  return {
    id: "installation-1",
    plugin_key: "com.example.hello",
    name: "Hello Panel",
    version: "1.0.0",
    source_url: "https://example.com/plugin/multica.plugin.json",
    enabled: true,
    granted_scopes: ["issues:read"],
    config_schema: [],
    config: {},
    configured_secrets: [],
    surfaces: [SURFACE],
    hooks: [],
    resources: [],
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

describe("surface connect-src", () => {
  it("derives the allowlist from granted net: scopes only", () => {
    expect(surfaceConnectSources(["issues:read", "net:example.com", "storage:user", "net:api.example.com"]))
      .toEqual(["https://example.com", "https://api.example.com"]);
  });

  it("gives a surface with no net: scope no third-party destination at all", () => {
    // 'none', not merely an empty allowlist. This bounds THIRD-PARTY hosts; the
    // author's own origin stays reachable through script-src, because the code
    // has to load from somewhere. Saying otherwise would overclaim.
    expect(buildSurfaceCSP(["issues:read"], "https://example.com")).toContain("connect-src 'none'");
  });

  it("keeps the cheapest side channels closed", () => {
    // <img> and webfont URLs are the two exfiltration paths that need no
    // scripting at all, so neither gets the script origin.
    const csp = buildSurfaceCSP(["net:example.com"], "https://cdn.example.com");
    expect(csp).toContain("img-src data: blob:");
    expect(csp).toContain("font-src data:");
    expect(csp).not.toContain("img-src https://cdn.example.com");
  });

  it("does not let an undeclared domain in through the script origin", () => {
    const csp = buildSurfaceCSP(["net:example.com"], "https://cdn.example.com");
    expect(csp).toContain("connect-src https://example.com");
    expect(csp).not.toContain("connect-src https://cdn.example.com");
  });

  it("denies everything by default and allows no plugin-controlled framing", () => {
    const csp = buildSurfaceCSP([], "https://example.com");
    expect(csp).toContain("default-src 'none'");
    // frame-ancestors is intentionally absent — <meta> ignores it, and the
    // sandbox already denies this document the ability to frame anything.
    expect(csp).not.toContain("frame-ancestors");
    expect(csp).toContain("base-uri 'none'");
    expect(csp).toContain("form-action 'none'");
  });
});

describe("surface entry resolution", () => {
  it("resolves the entry against the manifest URL, so the author's host serves the code", () => {
    expect(resolveSurfaceEntry(installation(), SURFACE)).toBe("https://example.com/plugin/ui/main.js");
  });

  it("refuses a local: install rather than inventing a URL", () => {
    // A local source lives on the server's filesystem; there is nothing a
    // browser could fetch, and guessing a URL would be worse than saying so.
    expect(resolveSurfaceEntry(installation({ source_url: "local:hello" }), SURFACE)).toBeNull();
  });

  it("refuses a plaintext source on a real host", () => {
    expect(resolveSurfaceEntry(installation({ source_url: "http://example.com/multica.plugin.json" }), SURFACE)).toBeNull();
  });

  it("allows plain HTTP from loopback so a surface can be developed locally", () => {
    // Browsers already treat loopback as a secure context. Without this a
    // plugin author cannot iterate on a surface at all: there is no way to
    // serve one over HTTPS from a laptop.
    expect(resolveSurfaceEntry(installation({ source_url: "http://localhost:8787/multica.plugin.json" }), SURFACE))
      .toBe("http://localhost:8787/ui/main.js");
    expect(resolveSurfaceEntry(installation({ source_url: "http://127.0.0.1:8787/multica.plugin.json" }), SURFACE))
      .toBe("http://127.0.0.1:8787/ui/main.js");
  });

  it("does not treat a hostname that merely contains localhost as loopback", () => {
    expect(resolveSurfaceEntry(installation({ source_url: "http://localhost.evil.test/multica.plugin.json" }), SURFACE)).toBeNull();
    expect(resolveSurfaceEntry(installation({ source_url: "http://notlocalhost/multica.plugin.json" }), SURFACE)).toBeNull();
  });
});

describe("surface document", () => {
  it("puts the policy before anything it governs", () => {
    const document = buildSurfaceDocument({
      entryUrl: "https://example.com/plugin/ui/main.js",
      grantedScopes: ["net:example.com"],
      theme: {},
    });
    const cspIndex = document.indexOf("Content-Security-Policy");
    const scriptIndex = document.indexOf("<script");
    expect(cspIndex).toBeGreaterThan(-1);
    expect(cspIndex).toBeLessThan(scriptIndex);
  });

  it("escapes the entry URL so a crafted manifest cannot break out of the attribute", () => {
    const document = buildSurfaceDocument({
      entryUrl: 'https://example.com/a"><script>alert(1)</script>.js',
      grantedScopes: [],
      theme: {},
    });
    expect(document).not.toContain('"><script>alert(1)');
    expect(document).toContain("&quot;");
  });

  it("forwards theme tokens as custom properties so a surface looks native untouched", () => {
    const document = buildSurfaceDocument({
      entryUrl: "https://example.com/ui/main.js",
      grantedScopes: [],
      theme: { "--background": "oklch(1 0 0)", "--radius": "6px" },
    });
    expect(document).toContain("--background: oklch(1 0 0);");
    expect(document).toContain("--radius: 6px;");
  });
});
