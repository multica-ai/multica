import { describe, expect, it } from "vitest";
import type { Workspace } from "@multica/core/types";
import { chooseWorkspace, workspaceSlugFromPath } from "./workspace";

const ws = (slug: string) => ({ id: `id-${slug}`, slug }) as Workspace;

describe("chooseWorkspace", () => {
  it("returns to the workspace the user was last in", () => {
    expect(chooseWorkspace([ws("acme"), ws("beta")], "beta")?.slug).toBe("beta");
  });

  it("falls back to the first workspace when the remembered one is gone", () => {
    expect(chooseWorkspace([ws("acme"), ws("beta")], "deleted")?.slug).toBe("acme");
    expect(chooseWorkspace([ws("acme")], null)?.slug).toBe("acme");
  });

  it("has nothing to show without workspaces", () => {
    expect(chooseWorkspace([], "acme")).toBeNull();
  });
});

describe("workspaceSlugFromPath", () => {
  it("reads the workspace from a core destination", () => {
    expect(workspaceSlugFromPath("/acme/issues")).toBe("acme");
    expect(workspaceSlugFromPath("/acme?x=1")).toBe("acme");
  });

  it("ignores app-level routes", () => {
    expect(workspaceSlugFromPath("/onboarding")).toBeNull();
    expect(workspaceSlugFromPath("/workspaces/new")).toBeNull();
    expect(workspaceSlugFromPath("/")).toBeNull();
  });
});
