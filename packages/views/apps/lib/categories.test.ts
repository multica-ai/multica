import { describe, expect, it } from "vitest";
import type { AppCatalogEntry } from "@multica/core/types";
import { formatCategoryName, groupAppsByCategory, knownCategory, normalizeCategory } from "./categories";

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

  it("folds Composio's synonyms of a curated category into it", () => {
    const of = (category: string) => knownCategory({ slug: "a", name: "a", curated: false, category });
    expect(of("artificial intelligence")).toBe("ai_ml");
    expect(of("sales & crm")).toBe("crm");
    expect(of("social media marketing")).toBe("marketing_social");
    expect(of("human resources")).toBe("hr_recruiting");
    expect(of("images & design")).toBe("design");
    expect(of("media generation")).toBe("ai_content_generation");
  });

  it("names every category Composio lists three or more toolkits under", () => {
    // From docs.composio.dev/toolkits, November 2026.
    const names = [
      "developer tools", "model context protocol", "artificial intelligence", "analytics", "crm",
      "marketing automation", "documents", "project management", "images & design", "email",
      "security & identity tools", "ecommerce", "accounting", "forms & surveys", "databases",
      "web scraping & browser", "productivity", "scheduling & booking", "human resources",
      "email newsletters", "communication", "business intelligence", "ai web scraping", "ai agents",
      "web search & research", "customer support", "ai sales tools", "ai content generation",
      "payment processing", "social media marketing", "server monitoring", "video & audio", "signatures",
      "phone & sms", "marketing", "sales & crm", "audio & speech", "ai chatbots", "ai document extraction",
      "url shortener", "people & contact data", "file management & storage", "hr talent & recruitment",
      "commerce", "notifications", "event management", "places & weather", "time tracking software",
      "team chat", "social media accounts", "media generation", "education", "documents & ocr",
      "market data", "it operations", "internet of things", "team collaboration", "spreadsheets",
      "fundraising", "ai models", "website builders", "transactional email", "task management", "reviews",
      "product management", "company data", "ads & conversion", "proposal & invoice management",
      "online courses", "news & lifestyle", "app builder", "ai meeting assistants", "taxes",
      "lifestyle & entertainment", "gaming", "content & files", "bookmark managers", "video conferencing",
      "transcription", "social & creator data", "seo & ai visibility", "contact management",
      "website & app building", "notes", "legal & public records", "drip emails",
      "ai safety compliance detection", "ai assistants",
    ];
    expect(names.filter((category) => !knownCategory({ slug: "a", name: "a", curated: false, category }))).toEqual([]);
  });

  it("title-cases Composio's lowercase names and keeps acronyms and its own casing", () => {
    expect(formatCategoryName("artificial intelligence")).toBe("Artificial Intelligence");
    expect(formatCategoryName("ai web scraping")).toBe("AI Web Scraping");
    expect(formatCategoryName("phone & sms")).toBe("Phone & SMS");
    expect(formatCategoryName("sales & crm")).toBe("Sales & CRM");
    expect(formatCategoryName("web search and research")).toBe("Web Search and Research");
    expect(formatCategoryName("e-commerce for ai/ml")).toBe("E-commerce for AI/ML");
    expect(formatCategoryName(" the model  context protocol ")).toBe("The Model Context Protocol");
    expect(formatCategoryName("Developer Tools & DevOps")).toBe("Developer Tools & DevOps");
  });

  it("sorts categories by size, keeps curated and sizeable ones and puts the rest in Other", () => {
    const { groups, categoryOf } = groupAppsByCategory([
      app("a", { category: "AI & Machine Learning" }),
      app("b", { category_id: "ai-machine-learning" }),
      app("c", { category: "CRM" }),
      app("d", { category: "Wi-Fi Management" }),
      app("e", { category: "Space Exploration" }),
      app("f", { category: "space exploration" }),
      app("g", { category: "Space Exploration" }),
      app("h", { category: "ai agents" }),
      app("i", { category: "ai agents" }),
      app("j", { category: "ai agents" }),
      app("k", { category: "databases" }),
      app("l"),
      { slug: "remote" },
    ]);
    expect(groups.map((g) => [g.key, g.slugs.length])).toEqual([
      ["ai_agents", 3],
      ["name:space_exploration", 3],
      ["ai_ml", 2],
      ["crm", 1],
      ["other", 4],
    ]);
    expect(groups[1]!.name).toBe("Space Exploration");
    expect(categoryOf.get("d")).toBe("other");
    expect(categoryOf.get("k")).toBe("other");
  });
});
