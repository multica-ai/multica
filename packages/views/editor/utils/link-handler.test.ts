import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { openLink, toInternalAppPath } from "./link-handler";

const APP_ORIGIN = "https://app.multica.ai";

function navigatedPaths(): string[] {
  return dispatched.map((e) => (e as CustomEvent<{ path: string }>).detail.path);
}

let dispatched: Event[] = [];
let openSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  dispatched = [];
  vi.spyOn(window, "dispatchEvent").mockImplementation((e: Event) => {
    dispatched.push(e);
    return true;
  });
  openSpy = vi.spyOn(window, "open").mockImplementation(() => null);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("toInternalAppPath", () => {
  it("returns the path (with search and hash) for a URL on the app origin", () => {
    expect(
      toInternalAppPath(`${APP_ORIGIN}/acme/issues/MUL-1?tab=a#c`, APP_ORIGIN),
    ).toBe("/acme/issues/MUL-1?tab=a#c");
  });

  it("returns null for another origin", () => {
    expect(toInternalAppPath("https://github.com/a/b/pull/1", APP_ORIGIN)).toBeNull();
  });

  it("returns null when the platform exposes no app origin", () => {
    expect(toInternalAppPath(`${APP_ORIGIN}/acme/issues/1`, null)).toBeNull();
  });

  it("keeps backend-served paths external so downloads and assets still work", () => {
    // Every one of these first segments is a reserved slug, which is exactly
    // why the reserved list — not a hand-kept deny-list — decides this.
    expect(
      toInternalAppPath(`${APP_ORIGIN}/api/attachments/abc/download`, APP_ORIGIN),
    ).toBeNull();
    expect(
      toInternalAppPath(`${APP_ORIGIN}/uploads/2026/07/report.pdf`, APP_ORIGIN),
    ).toBeNull();
    expect(toInternalAppPath(`${APP_ORIGIN}/uploads`, APP_ORIGIN)).toBeNull();
    expect(toInternalAppPath(`${APP_ORIGIN}/_next/static/x.js`, APP_ORIGIN)).toBeNull();
    expect(toInternalAppPath(`${APP_ORIGIN}/favicon.ico`, APP_ORIGIN)).toBeNull();
  });

  it("keeps pre-workspace and root paths external — they are not workspace pages", () => {
    expect(toInternalAppPath(`${APP_ORIGIN}/login`, APP_ORIGIN)).toBeNull();
    expect(toInternalAppPath(`${APP_ORIGIN}/auth/callback`, APP_ORIGIN)).toBeNull();
    expect(toInternalAppPath(`${APP_ORIGIN}/`, APP_ORIGIN)).toBeNull();
  });

  it("ignores case and percent-encoding when matching a reserved first segment", () => {
    expect(toInternalAppPath(`${APP_ORIGIN}/UPLOADS/x.pdf`, APP_ORIGIN)).toBeNull();
    expect(toInternalAppPath(`${APP_ORIGIN}/%75ploads/x.pdf`, APP_ORIGIN)).toBeNull();
  });

  it("returns null for non-http schemes and unparseable hrefs", () => {
    expect(toInternalAppPath("mailto:a@b.com", APP_ORIGIN)).toBeNull();
    expect(toInternalAppPath("not a url", APP_ORIGIN)).toBeNull();
  });
});

describe("openLink", () => {
  it("navigates in-app for a URL pointing back at this deployment (MUL-5208)", () => {
    openLink(`${APP_ORIGIN}/acme/issues/MUL-1`, "acme", APP_ORIGIN);
    expect(navigatedPaths()).toEqual(["/acme/issues/MUL-1"]);
    expect(openSpy).not.toHaveBeenCalled();
  });

  it("navigates in-app for a cross-workspace app URL without rewriting the slug", () => {
    openLink(`${APP_ORIGIN}/other/issues/MUL-1`, "acme", APP_ORIGIN);
    expect(navigatedPaths()).toEqual(["/other/issues/MUL-1"]);
  });

  it("opens an external URL in a new window", () => {
    openLink("https://github.com/multica-ai/multica/pull/1", "acme", APP_ORIGIN);
    expect(dispatched).toHaveLength(0);
    expect(openSpy).toHaveBeenCalledWith(
      "https://github.com/multica-ai/multica/pull/1",
      "_blank",
      "noopener,noreferrer",
    );
  });

  it("still opens an app URL externally when no app origin is known", () => {
    openLink(`${APP_ORIGIN}/acme/issues/MUL-1`, "acme");
    expect(dispatched).toHaveLength(0);
    expect(openSpy).toHaveBeenCalled();
  });

  it("prefixes the current slug on a slugless workspace path", () => {
    openLink("/issues/MUL-1", "acme", APP_ORIGIN);
    expect(navigatedPaths()).toEqual(["/acme/issues/MUL-1"]);
  });

  it("leaves a path that already carries a slug alone", () => {
    openLink("/other/issues/MUL-1", "acme", APP_ORIGIN);
    expect(navigatedPaths()).toEqual(["/other/issues/MUL-1"]);
  });
});
