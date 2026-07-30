import { describe, it, expect } from "vitest";
import { routeIconForPath } from "../../layout/route-icon-components";
import { SkillIcon } from "./skill-icon";

describe("SkillIcon", () => {
  // The sidebar and desktop tab bar both render the route icon, while every
  // in-page surface renders SkillIcon. Before they were derived from one
  // declaration the product showed four icons for the same entity
  // (BookOpenText, BookOpen, FileText, Sparkles).
  it("is the same component the sidebar and tab bar resolve for /skills", () => {
    expect(SkillIcon).toBe(routeIconForPath("/acme/skills"));
  });

  it("keeps matching for a nested skill detail path", () => {
    expect(SkillIcon).toBe(routeIconForPath("/acme/skills/skill-1"));
  });
});
