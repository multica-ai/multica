import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  openLink,
  parseWorkspaceEntityLink,
  toInternalAppPath,
} from "./link-handler";

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

describe("parseWorkspaceEntityLink", () => {
  const PROJECT_ID = "8f14e45f-ceea-4d0e-a1a2-9b1c0d3e4f5a";
  const ISSUE_ID = "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4bed";

  it("parses an absolute project URL on the app origin", () => {
    expect(
      parseWorkspaceEntityLink(
        `${APP_ORIGIN}/acme/projects/${PROJECT_ID}`,
        APP_ORIGIN,
      ),
    ).toEqual({ kind: "project", id: PROJECT_ID, slug: "acme" });
  });

  it("parses an absolute issue URL on the app origin", () => {
    expect(
      parseWorkspaceEntityLink(`${APP_ORIGIN}/acme/issues/${ISSUE_ID}`, APP_ORIGIN),
    ).toEqual({ kind: "issue", id: ISSUE_ID, slug: "acme" });
  });

  it("parses a site-relative path without needing an app origin", () => {
    expect(parseWorkspaceEntityLink(`/acme/projects/${PROJECT_ID}`)).toEqual({
      kind: "project",
      id: PROJECT_ID,
      slug: "acme",
    });
  });

  it("reports a null slug for the slugless legacy form", () => {
    expect(parseWorkspaceEntityLink(`/projects/${PROJECT_ID}`)).toEqual({
      kind: "project",
      id: PROJECT_ID,
      slug: null,
    });
  });

  it("returns null for another origin", () => {
    expect(
      parseWorkspaceEntityLink(
        `https://evil.example/acme/projects/${PROJECT_ID}`,
        APP_ORIGIN,
      ),
    ).toBeNull();
  });

  // A leading slash does not mean "this site". Both of these name another host
  // and a browser follows them there, so the parser has to resolve the href
  // rather than test its prefix.
  it("returns null for a host-bearing href that still starts with a slash", () => {
    expect(
      parseWorkspaceEntityLink(`//evil.example/projects/${PROJECT_ID}`, APP_ORIGIN),
    ).toBeNull();
    // Backslashes are normalised to slashes, so this names evil.example too —
    // and it slips past a `//` prefix test.
    expect(
      parseWorkspaceEntityLink(`/\\evil.example/projects/${PROJECT_ID}`, APP_ORIGIN),
    ).toBeNull();
  });

  it("returns null for a non-http scheme", () => {
    expect(parseWorkspaceEntityLink("javascript:alert(1)", APP_ORIGIN)).toBeNull();
  });

  // The slugless form resolves against the current workspace either way; the
  // two spellings must not disagree about that.
  it("treats the slugless form the same whether or not it carries the origin", () => {
    expect(
      parseWorkspaceEntityLink(`${APP_ORIGIN}/projects/${PROJECT_ID}`, APP_ORIGIN),
    ).toEqual({ kind: "project", id: PROJECT_ID, slug: null });
  });

  it("returns null for a list page", () => {
    expect(parseWorkspaceEntityLink("/acme/projects")).toBeNull();
  });

  it("returns null for a deeper route under the entity", () => {
    expect(
      parseWorkspaceEntityLink(`/acme/projects/${PROJECT_ID}/settings`),
    ).toBeNull();
  });

  it("returns null for an entity route this parser has no chip for", () => {
    expect(parseWorkspaceEntityLink(`/acme/agents/${PROJECT_ID}`)).toBeNull();
  });

  // A query string or fragment addresses something narrower than the entity
  // page, and a chip cannot carry it.
  it("returns null when the link carries a query string or fragment", () => {
    expect(
      parseWorkspaceEntityLink(`/acme/projects/${PROJECT_ID}?tab=issues`),
    ).toBeNull();
    expect(
      parseWorkspaceEntityLink(`/acme/issues/${ISSUE_ID}#comment-3`),
    ).toBeNull();
  });

  // `copyLink` / `openInNewTab` build `paths.issueDetail(identifier || id)` and
  // the issue route rewrites a UUID URL back to the identifier, so this — not
  // the UUID form — is what a user actually copies out of the app.
  it("parses an issue addressed by identifier", () => {
    expect(parseWorkspaceEntityLink("/acme/issues/MUL-1")).toEqual({
      kind: "issue",
      id: "MUL-1",
      slug: "acme",
    });
  });

  // A project has no shorthand, so an identifier-shaped id under /projects/
  // addresses nothing this parser could resolve.
  it("returns null for an identifier-shaped project id", () => {
    expect(parseWorkspaceEntityLink("/acme/projects/MUL-1")).toBeNull();
  });

  it("returns null for an id that is neither a UUID nor an identifier", () => {
    expect(parseWorkspaceEntityLink("/acme/issues/roadmap")).toBeNull();
    // Lowercase is not the identifier form — matching it would turn ordinary
    // hyphenated path segments into entity references.
    expect(parseWorkspaceEntityLink("/acme/issues/mul-1")).toBeNull();
  });

  it("returns null when the slug position holds a reserved slug", () => {
    expect(parseWorkspaceEntityLink(`/login/projects/${PROJECT_ID}`)).toBeNull();
  });

  it("returns null for a malformed percent-escape", () => {
    expect(parseWorkspaceEntityLink("/acme/projects/%E0%A4%A")).toBeNull();
  });
});
