import { describe, it, expect } from "vitest";
import { paths, isGlobalPath } from "./paths";

describe("paths.workspace(slug)", () => {
  const ws = paths.workspace("acme");

  it("builds dashboard paths with slug prefix", () => {
    expect(ws.issues()).toBe("/acme/issues");
    expect(ws.issueDetail("abc-123")).toBe("/acme/issues/abc-123");
    expect(ws.projects()).toBe("/acme/projects");
    expect(ws.projectDetail("p1")).toBe("/acme/projects/p1");
    expect(ws.autopilots()).toBe("/acme/autopilots");
    expect(ws.autopilotDetail("a1")).toBe("/acme/autopilots/a1");
    expect(ws.agents()).toBe("/acme/agents");
    expect(ws.inbox()).toBe("/acme/inbox");
    expect(ws.myIssues()).toBe("/acme/my-issues");
    expect(ws.runtimes()).toBe("/acme/runtimes");
    expect(ws.skills()).toBe("/acme/skills");
    expect(ws.skillDetail("skl_123")).toBe("/acme/skills/skl_123");
    expect(ws.settings()).toBe("/acme/settings");
  });

  it("URL-encodes special characters in ids", () => {
    expect(ws.issueDetail("id with space")).toBe("/acme/issues/id%20with%20space");
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
