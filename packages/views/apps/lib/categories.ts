import type { AppCatalogEntry } from "@multica/core/types";

/** Categories the Apps page names in every locale, in Composio's order. */
export const APP_CATEGORIES = [
  "ai_ml",
  "developer_tools",
  "data_analytics",
  "productivity",
  "collaboration",
  "marketing_social",
  "documents",
  "sales_support",
  "crm",
  "finance",
  "workflow_automation",
  "design",
  "ecommerce",
  "advertising",
  "scheduling",
  "entertainment",
  "hr_recruiting",
  "education",
  "other",
] as const;

export type KnownAppCategory = (typeof APP_CATEGORIES)[number];

// Normalized Composio ids and names; both "Developer Tools & DevOps" and
// "developer-tools-and-devops" normalize to "developer_tools_devops".
const ALIASES: Record<string, KnownAppCategory> = {
  ai_machine_learning: "ai_ml",
  ai: "ai_ml",
  machine_learning: "ai_ml",
  developer_tools_devops: "developer_tools",
  developer_tools: "developer_tools",
  devops: "developer_tools",
  data_analytics: "data_analytics",
  analytics: "data_analytics",
  productivity_project_management: "productivity",
  productivity: "productivity",
  project_management: "productivity",
  collaboration_communication: "collaboration",
  collaboration: "collaboration",
  communication: "collaboration",
  marketing_social_media: "marketing_social",
  marketing: "marketing_social",
  social_media: "marketing_social",
  document_file_management: "documents",
  documents: "documents",
  file_management: "documents",
  sales_customer_support: "sales_support",
  customer_support: "sales_support",
  sales: "sales_support",
  crm: "crm",
  finance_accounting: "finance",
  finance: "finance",
  accounting: "finance",
  workflow_automation: "workflow_automation",
  automation: "workflow_automation",
  design_creative_tools: "design",
  design: "design",
  e_commerce: "ecommerce",
  ecommerce: "ecommerce",
  advertising_marketing: "advertising",
  advertising: "advertising",
  scheduling_booking: "scheduling",
  scheduling: "scheduling",
  entertainment_media: "entertainment",
  entertainment: "entertainment",
  hr_recruiting: "hr_recruiting",
  recruiting: "hr_recruiting",
  education_lms: "education",
  education: "education",
};

/** A category Multica has no label for keeps Composio's name once this many apps share it. */
const MIN_UNNAMED_CATEGORY = 3;

export function normalizeCategory(value: string): string {
  return value
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter((word) => word && word !== "and")
    .join("_");
}

export function knownCategory(entry: AppCatalogEntry | undefined): KnownAppCategory | null {
  for (const raw of [entry?.category_id, entry?.category]) {
    if (!raw) continue;
    const key = normalizeCategory(raw);
    const known = ALIASES[key] ?? APP_CATEGORIES.find((category) => category === key);
    if (known) return known;
  }
  return null;
}

export interface AppCategoryGroup {
  /** A known category, or `name:<normalized>` for one only Composio names. */
  key: string;
  known: KnownAppCategory | null;
  /** Composio's name, for categories without a translation. */
  name: string;
  slugs: string[];
}

/**
 * Sorts apps into categories: largest first, "other" last. Apps without a
 * category, or in a small one Multica has no label for, go to "other".
 */
export function groupAppsByCategory(
  apps: { slug: string; entry?: AppCatalogEntry }[],
): { groups: AppCategoryGroup[]; categoryOf: Map<string, string> } {
  const byKey = new Map<string, AppCategoryGroup>();
  const add = (key: string, known: KnownAppCategory | null, name: string, slug: string) => {
    const group = byKey.get(key) ?? { key, known, name, slugs: [] };
    group.slugs.push(slug);
    byKey.set(key, group);
  };
  for (const app of apps) {
    const known = knownCategory(app.entry);
    const name = app.entry?.category?.trim() ?? "";
    if (known) add(known, known, "", app.slug);
    else if (name) add(`name:${normalizeCategory(name)}`, null, name, app.slug);
    else add("other", "other", "", app.slug);
  }
  for (const group of [...byKey.values()]) {
    if (group.known || group.slugs.length >= MIN_UNNAMED_CATEGORY) continue;
    byKey.delete(group.key);
    for (const slug of group.slugs) add("other", "other", "", slug);
  }
  const groups = [...byKey.values()].sort(
    (a, b) =>
      Number(a.key === "other") - Number(b.key === "other") ||
      b.slugs.length - a.slugs.length ||
      a.key.localeCompare(b.key),
  );
  const categoryOf = new Map<string, string>();
  for (const group of groups) for (const slug of group.slugs) categoryOf.set(slug, group.key);
  return { groups, categoryOf };
}
