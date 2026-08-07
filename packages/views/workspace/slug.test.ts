import { describe, it, expect } from "vitest";
import {
  nameToWorkspaceSlug,
  randomCelestialWorkspaceIdentity,
} from "./slug";
import { CELESTIAL_WORKSPACE_NAMES } from "./celestial-workspace-names";

describe("nameToWorkspaceSlug", () => {
  it("lowercases ASCII names and joins words with hyphens", () => {
    expect(nameToWorkspaceSlug("My Team")).toBe("my-team");
    expect(nameToWorkspaceSlug("Acme Inc")).toBe("acme-inc");
    expect(nameToWorkspaceSlug("Project X-1")).toBe("project-x-1");
  });

  it("strips leading and trailing hyphens", () => {
    expect(nameToWorkspaceSlug("---test---")).toBe("test");
    expect(nameToWorkspaceSlug("  Hello  ")).toBe("hello");
  });

  it("collapses multiple non-alphanumeric runs into a single hyphen", () => {
    expect(nameToWorkspaceSlug("foo!!!bar")).toBe("foo-bar");
    expect(nameToWorkspaceSlug("a.b.c")).toBe("a-b-c");
  });

  // Regression: previously fell back to literal "workspace" — caused two
  // separate non-ASCII-named workspaces on the same instance to 409 (slug
  // taken) and silently surfaced a confusing "/workspace/issues" URL.
  it("returns empty string for non-ASCII-only names", () => {
    expect(nameToWorkspaceSlug("测试")).toBe("");
    expect(nameToWorkspaceSlug("こんにちは")).toBe("");
    expect(nameToWorkspaceSlug("🚀")).toBe("");
    expect(nameToWorkspaceSlug("مرحبا")).toBe("");
  });

  it("returns empty string for symbol-only names", () => {
    expect(nameToWorkspaceSlug("---")).toBe("");
    expect(nameToWorkspaceSlug("!!!")).toBe("");
    expect(nameToWorkspaceSlug("   ")).toBe("");
  });

  it("preserves ASCII characters even when mixed with non-ASCII", () => {
    expect(nameToWorkspaceSlug("测试 Team")).toBe("team");
    expect(nameToWorkspaceSlug("Project 测试 1")).toBe("project-1");
  });
});

describe("randomCelestialWorkspaceIdentity", () => {
  it("picks a celestial name and appends a four-character slug suffix", () => {
    const values = [0, 0, 0.5, 0.999, 0.25];
    let index = 0;

    const identity = randomCelestialWorkspaceIdentity(
      "en",
      () => values[index++] ?? 0,
    );

    expect(identity.name).toBe(CELESTIAL_WORKSPACE_NAMES[0]?.names.en);
    expect(identity.slug).toMatch(/^alpha-centauri-[a-z0-9]{4}$/);
    expect(identity.slug).toBe("alpha-centauri-as9j");
  });

  it("keeps the source list unique", () => {
    expect(
      new Set(CELESTIAL_WORKSPACE_NAMES.map(({ slugBase }) => slugBase)).size,
    ).toBe(
      CELESTIAL_WORKSPACE_NAMES.length,
    );
  });
});
