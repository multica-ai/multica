import { describe, expect, it } from "vitest";
import type { Workspace } from "../types";
import { paths } from "./paths";
import { resolvePostAuthDestination } from "./resolve";

function makeWs(slug: string): Workspace {
  return {
    id: `id-${slug}`,
    name: slug,
    slug,
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: slug.toUpperCase(),
    avatar_url: null,
    created_at: "",
    updated_at: "",
  };
}

describe("resolvePostAuthDestination", () => {
  it("enters the first existing workspace without consulting onboarded_at", () => {
    const ws = [makeWs("acme"), makeWs("beta")];
    expect(resolvePostAuthDestination(ws)).toBe(
      paths.workspace("acme").issues(),
    );
  });

  it("uses the authority-backed workspace entry when none is projected", () => {
    expect(resolvePostAuthDestination([])).toBe(paths.newWorkspace());
  });
});
