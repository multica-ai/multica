import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import Mention from "@tiptap/extension-mention";
import type { SkillSummary } from "@multica/core/types";
import { createSkillSuggestion, createSkillMentionExtension } from "./extension";

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

  // Regression: FIR-2301. While the `/` popup is open, Enter must insert the
  // highlighted skill — not submit the chat. The chat composer's submitShortcut
  // extension binds Enter at the default priority (100). Tiptap runs the plugins
  // of higher-priority extensions first and the first handleKeyDown that returns
  // true wins, so this extension must sit at >= the upstream `@` mention's
  // priority (101) to intercept Enter before submitShortcut. At the default
  // priority Enter submitted the chat as raw text instead of selecting the skill.
  it("matches @ mention priority so the / popup wins Enter over submit-shortcut", () => {
    const qc = new QueryClient();
    const ext = createSkillMentionExtension(qc);
    expect(ext.config.priority).toBe(Mention.config.priority);
    expect(ext.config.priority).toBeGreaterThan(100);
  });
});
