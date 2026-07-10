import { describe, it, expect } from "vitest";
import { paths, isGlobalPath } from "./paths";

describe("paths.workspace(slug)", () => {
  const ws = paths.workspace("acme");

  it("builds workspace paths with slug prefix", () => {
    expect(ws.usage()).toBe("/acme/usage");
    expect(ws.issues()).toBe("/acme/issues");
    expect(ws.issueDetail("abc-123")).toBe("/acme/issues/abc-123");
    expect(ws.referenceObject("github_pr", "org/repo#42")).toBe(
      "/acme/cerebro/references/github_pr/org%2Frepo%2342",
    );
    expect(ws.projects()).toBe("/acme/projects");
    expect(ws.projectDetail("p1")).toBe("/acme/projects/p1");
    expect(ws.autopilots()).toBe("/acme/autopilots");
    expect(ws.autopilotDetail("a1")).toBe("/acme/autopilots/a1");
    expect(ws.agents()).toBe("/acme/agents");
    expect(ws.memberDetail("u1")).toBe("/acme/members/u1");
    expect(ws.inbox()).toBe("/acme/inbox");
    expect(ws.search()).toBe("/acme/search");
    expect(ws.myIssues()).toBe("/acme/my-issues");
    expect(ws.runtimes()).toBe("/acme/runtimes");
    expect(ws.skills()).toBe("/acme/skills");
    expect(ws.skillDetail("skl_123")).toBe("/acme/skills/skl_123");
    expect(ws.squads()).toBe("/acme/squads");
    expect(ws.squadDetail("sq_1")).toBe("/acme/squads/sq_1");
    expect(ws.settings()).toBe("/acme/settings");
    // FIR-2595: shareable per-note URL.
    expect(ws.notes()).toBe("/acme/notes");
    expect(ws.noteDetail("n1")).toBe("/acme/notes/n1");
    // FIR-2688: shareable per-folder URLs across the four folder surfaces.
    expect(ws.documentsFolder("f1")).toBe("/acme/documents?folder=f1");
    expect(ws.notesFolder("f1")).toBe("/acme/notes?folder=f1");
    expect(ws.skillsFolder("f1")).toBe("/acme/skills?folder=f1");
    expect(ws.autopilotsFolder("f1")).toBe("/acme/autopilots?folder=f1");
  });

  it("URL-encodes special characters in ids", () => {
    expect(ws.issueDetail("id with space")).toBe("/acme/issues/id%20with%20space");
    expect(ws.noteDetail("id with space")).toBe("/acme/notes/id%20with%20space");
    // FIR-2688: ids are opaque; encode so a weird id can't break the query.
    expect(ws.notesFolder("a/b?c")).toBe("/acme/notes?folder=a%2Fb%3Fc");
    expect(ws.skillsFolder("a b")).toBe("/acme/skills?folder=a%20b");
  });
});

describe("paths (global)", () => {
  it("builds global paths without slug", () => {
    expect(paths.login()).toBe("/login");
    expect(paths.newWorkspace()).toBe("/workspaces/new");
    expect(paths.invite("inv-1")).toBe("/invite/inv-1");
    expect(paths.authCallback()).toBe("/auth/callback");
  });
});

describe("isGlobalPath", () => {
  it("returns true for pre-workspace routes", () => {
    expect(isGlobalPath("/login")).toBe(true);
    expect(isGlobalPath("/workspaces/new")).toBe(true);
    expect(isGlobalPath("/invite/abc")).toBe(true);
    expect(isGlobalPath("/auth/callback")).toBe(true);
  });

  it("returns false for workspace-scoped paths", () => {
    expect(isGlobalPath("/acme/issues")).toBe(false);
    expect(isGlobalPath("/")).toBe(false);
  });
});
