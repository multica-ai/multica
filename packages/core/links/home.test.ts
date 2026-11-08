// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { WorkspaceLink } from "../types/workspace-link";
import { findHomeLink, workspaceLandingPath } from "./home";

const home: WorkspaceLink = {
  id: "home",
  workspaceId: "ws",
  projectId: null,
  name: "Home",
  icon: "",
  urlTemplate: "https://example.com/{workspace_slug}",
  openMode: "embed",
  allowSameOrigin: false,
  position: 0,
  isHome: true,
};

describe("home links", () => {
  it("resolves a workspace home to the existing embedded route", () => {
    expect(workspaceLandingPath("my team", [home])).toBe("/my%20team/links/home");
  });
  it.each([
    [],
    [{ ...home, isHome: false }],
    [{ ...home, projectId: "project" }],
    [{ ...home, openMode: "new_tab" as const }],
    [{ ...home, urlTemplate: "http://example.com" }],
  ])("keeps the issues fallback for %j", (...links) => {
    expect(workspaceLandingPath("acme", links)).toBe("/acme/issues");
  });
  it("finds a project home without mixing its workspace scope", () => {
    const project = { ...home, projectId: "project", urlTemplate: "https://example.com/{project_id}" };
    expect(findHomeLink([project])).toBe(project);
    expect(workspaceLandingPath("acme", [project, home])).toBe("/acme/links/home");
  });
});
