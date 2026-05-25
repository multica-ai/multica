import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { SkillSummary } from "@multica/core/types";
import { createSkillSuggestion } from "./extension";

const listSkillsMock = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/platform", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/platform")>()),
  getCurrentWsId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    get listSkills() {
      return listSkillsMock;
    },
  },
}));

function makeSkill(overrides: Partial<SkillSummary>): SkillSummary {
  return {
    id: "skill-1",
    name: "Release notes",
    description: "Draft customer-facing release notes",
    created_at: "2026-05-25T00:00:00Z",
    updated_at: "2026-05-25T00:00:00Z",
    created_by: null,
    owner_id: null,
    approver_ids: [],
    current_version: "1",
    workspace_id: "ws-1",
    config: {},
    ...overrides,
  };
}

describe("createSkillSuggestion", () => {
  afterEach(() => {
    listSkillsMock.mockReset();
  });

  it("loads workspace skills on the first / popup when the cache is cold", async () => {
    listSkillsMock.mockResolvedValue([
      makeSkill({ id: "skill-release", name: "Release notes" }),
      makeSkill({
        id: "skill-debug",
        name: "Systematic debugging",
        description: "Trace root causes",
      }),
    ]);
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    const config = createSkillSuggestion(qc);
    const items = await config.items!({ query: "release", editor: {} as never });

    expect(listSkillsMock).toHaveBeenCalledWith({ workspace_id: "ws-1" });
    expect(items).toEqual([
      {
        id: "skill-release",
        name: "Release notes",
        description: "Draft customer-facing release notes",
      },
    ]);
  });
});
