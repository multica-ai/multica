import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  describeBlockedNavigation,
  installNavigationGuard,
  isTrustedRendererURL,
  type NavigationGuardWindow,
} from "./navigation-guard";

const DEV_RENDERER = "http://localhost:5173/";
const PROD_RENDERER = "file:///Applications/Multica.app/Contents/renderer/index.html";

describe("isTrustedRendererURL", () => {
  describe("dev server origin", () => {
    it("trusts the renderer origin, including deeper paths on it", () => {
      expect(isTrustedRendererURL(DEV_RENDERER, DEV_RENDERER)).toBe(true);
      expect(isTrustedRendererURL("http://localhost:5173/index.html", DEV_RENDERER)).toBe(true);
    });

    it("rejects a different origin", () => {
      expect(isTrustedRendererURL("https://evil.example/", DEV_RENDERER)).toBe(false);
      // Same host, different port — a different origin.
      expect(isTrustedRendererURL("http://localhost:9999/", DEV_RENDERER)).toBe(false);
      // Same host and port, different scheme.
      expect(isTrustedRendererURL("https://localhost:5173/", DEV_RENDERER)).toBe(false);
    });

    it("rejects a file URL while the dev server is the trusted origin", () => {
      expect(isTrustedRendererURL("file:///Users/me/shot.png", DEV_RENDERER)).toBe(false);
    });
  });

  describe("packaged renderer", () => {
    it("trusts the exact renderer document", () => {
      expect(isTrustedRendererURL(PROD_RENDERER, PROD_RENDERER)).toBe(true);
    });

    it("rejects any other file, including a sibling of the renderer", () => {
      // A file:// document has an opaque origin, so origin equality would say
      // "same origin" for every file on disk. Path comparison is what makes
      // this safe.
      expect(
        isTrustedRendererURL(
          "file:///Applications/Multica.app/Contents/renderer/other.html",
          PROD_RENDERER,
        ),
      ).toBe(false);
      expect(isTrustedRendererURL("file:///Users/me/Desktop/shot.png", PROD_RENDERER)).toBe(false);
      expect(isTrustedRendererURL("file:///etc/passwd", PROD_RENDERER)).toBe(false);
    });

    it("rejects a remote origin", () => {
      expect(isTrustedRendererURL("https://evil.example/", PROD_RENDERER)).toBe(false);
    });
  });

  it("rejects unparseable input", () => {
    expect(isTrustedRendererURL("not a url", PROD_RENDERER)).toBe(false);
    expect(isTrustedRendererURL("", PROD_RENDERER)).toBe(false);
  });
});

describe("describeBlockedNavigation", () => {
  it("logs the origin only, never the path", () => {
    // A blocked URL is attacker- or agent-controlled; its path can spell out a
    // local filesystem layout.
    expect(describeBlockedNavigation("https://evil.example/secret/path?token=abc")).toBe(
      "https://evil.example",
    );
    expect(describeBlockedNavigation("file:///Users/me/Desktop/private.png")).toBe("file:");
    expect(describeBlockedNavigation("nonsense")).toBe("invalid URL");
  });
});

describe("installNavigationGuard", () => {
  let listener: (event: { preventDefault(): void }, url: string) => void;
  let window: NavigationGuardWindow;

  beforeEach(() => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    window = {
      webContents: {
        on: (_event, fn) => {
          listener = fn;
          return window;
        },
      },
    };
    installNavigationGuard(window, PROD_RENDERER);
  });

  function navigate(url: string): boolean {
    const preventDefault = vi.fn();
    listener({ preventDefault }, url);
    return preventDefault.mock.calls.length > 0;
  }

  it("allows a navigation to the renderer document itself", () => {
    expect(navigate(PROD_RENDERER)).toBe(false);
  });

  it("blocks a navigation to a foreign origin", () => {
    expect(navigate("https://evil.example/")).toBe(true);
  });

  it("blocks a navigation to an arbitrary local file", () => {
    expect(navigate("file:///Users/me/Desktop/shot.png")).toBe(true);
  });
});
