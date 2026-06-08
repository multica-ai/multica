import { describe, expect, it } from "vitest";
import type { SkillSummary } from "@multica/core/types";
import {
  UNCATEGORIZED_SKILL_CATEGORY,
  skillCategoryKey,
} from "./skill-category-filter";

function skill(category?: string): SkillSummary {
  return {
    id: "skill-1",
    workspace_id: "workspace-1",
    name: "demo",
    description: "Demo skill",
    config: {},
    created_by: null,
    created_at: "2026-06-08T00:00:00Z",
    updated_at: "2026-06-08T00:00:00Z",
    metadata: category === undefined ? {} : { category },
  } as SkillSummary;
}

describe("skillCategoryKey", () => {
  it("groups skills without category into a visible bucket", () => {
    expect(skillCategoryKey(skill())).toBe(UNCATEGORIZED_SKILL_CATEGORY);
    expect(skillCategoryKey(skill(""))).toBe(UNCATEGORIZED_SKILL_CATEGORY);
    expect(skillCategoryKey(skill("   "))).toBe(UNCATEGORIZED_SKILL_CATEGORY);
  });

  it("keeps real categories selectable", () => {
    expect(skillCategoryKey(skill("Analyse"))).toBe("Analyse");
  });
});
