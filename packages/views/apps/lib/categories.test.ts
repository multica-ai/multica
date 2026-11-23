import { describe, expect, it } from "vitest";
import type { AppCatalogEntry } from "@multica/core/types";
import { groupAppsByCategory, knownCategory, normalizeCategory } from "./categories";

const app = (slug: string, entry: Partial<AppCatalogEntry> = {}) => ({
  slug,
  entry: { slug, name: slug, curated: false, ...entry },
});

describe("app categories", () => {
  it("reads Composio ids and names alike", () => {
    expect(normalizeCategory("Developer Tools & DevOps")).toBe("developer_tools_devops");
    expect(normalizeCategory("developer-tools-and-devops")).toBe("developer_tools_devops");
    expect(knownCategory({ slug: "a", name: "a", curated: false, category_id: "e-commerce" })).toBe("ecommerce");
    expect(knownCategory({ slug: "a", name: "a", curated: false, category: "Social Media" })).toBe("marketing_social");
    expect(knownCategory({ slug: "a", name: "a", curated: false, category_id: "x", category: "CRM" })).toBe("crm");
  });

  it("sorts categories by size, keeps sizeable unknown ones and puts the rest in Other", () => {
    const { groups, categoryOf } = groupAppsByCategory([
      app("a", { category: "AI & Machine Learning" }),
      app("b", { category_id: "ai-machine-learning" }),
      app("c", { category: "CRM" }),
      app("d", { category: "Wi-Fi Management" }),
      app("e", { category: "Fundraising" }),
      app("f", { category: "Fundraising" }),
      app("g", { category: "Fundraising" }),
      app("h"),
      { slug: "remote" },
    ]);
    expect(groups.map((g) => [g.key, g.slugs.length])).toEqual([
      ["name:fundraising", 3],
      ["ai_ml", 2],
      ["crm", 1],
      ["other", 3],
    ]);
    expect(groups[0]!.name).toBe("Fundraising");
    expect(categoryOf.get("d")).toBe("other");
  });
});
