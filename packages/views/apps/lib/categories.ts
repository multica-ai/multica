import type { AppCatalogEntry } from "@multica/core/types";

/** Composio's curated categories, in its order. The Apps page lists each one it has apps for. */
const CURATED_CATEGORIES = [
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

/** Composio's long-tail categories, listed once enough apps share one. */
const LONG_TAIL_CATEGORIES = [
  "model_context_protocol",
  "marketing_automation",
  "email",
  "security_identity_tools",
  "forms_surveys",
  "databases",
  "web_scraping_browser",
  "email_newsletters",
  "business_intelligence",
  "ai_web_scraping",
  "ai_agents",
  "web_search_research",
  "ai_sales_tools",
  "ai_content_generation",
  "payment_processing",
  "server_monitoring",
  "video_audio",
  "signatures",
  "phone_sms",
  "audio_speech",
  "ai_chatbots",
  "ai_document_extraction",
  "url_shortener",
  "people_contact_data",
  "notifications",
  "event_management",
  "places_weather",
  "market_data",
  "it_operations",
  "internet_of_things",
  "spreadsheets",
  "fundraising",
  "website_builders",
  "reviews",
  "company_data",
  "ai_meeting_assistants",
  "bookmark_managers",
  "social_creator_data",
  "seo_ai_visibility",
  "legal_public_records",
  "ai_safety",
] as const;

/** Categories the Apps page names in every locale. */
export const APP_CATEGORIES = [...CURATED_CATEGORIES, ...LONG_TAIL_CATEGORIES] as const;

export type KnownAppCategory = (typeof APP_CATEGORIES)[number];

const CURATED = new Set<KnownAppCategory>(CURATED_CATEGORIES);

// Normalized Composio names and ids that map to another key. Synonyms of a
// curated category join it.
const ALIASES: Record<string, KnownAppCategory> = {
  ai_machine_learning: "ai_ml",
  ai: "ai_ml",
  machine_learning: "ai_ml",
  artificial_intelligence: "ai_ml",
  ai_models: "ai_ml",
  developer_tools_devops: "developer_tools",
  devops: "developer_tools",
  analytics: "data_analytics",
  productivity_project_management: "productivity",
  project_management: "productivity",
  product_management: "productivity",
  task_management: "productivity",
  time_tracking_software: "productivity",
  notes: "productivity",
  collaboration_communication: "collaboration",
  communication: "collaboration",
  team_collaboration: "collaboration",
  team_chat: "collaboration",
  video_conferencing: "collaboration",
  marketing_social_media: "marketing_social",
  marketing: "marketing_social",
  social_media: "marketing_social",
  social_media_marketing: "marketing_social",
  social_media_accounts: "marketing_social",
  document_file_management: "documents",
  file_management: "documents",
  file_management_storage: "documents",
  content_files: "documents",
  documents_ocr: "documents",
  sales_customer_support: "sales_support",
  customer_support: "sales_support",
  sales: "sales_support",
  sales_crm: "crm",
  contact_management: "crm",
  finance_accounting: "finance",
  accounting: "finance",
  taxes: "finance",
  proposal_invoice_management: "finance",
  automation: "workflow_automation",
  design_creative_tools: "design",
  images_design: "design",
  e_commerce: "ecommerce",
  commerce: "ecommerce",
  advertising_marketing: "advertising",
  ads_conversion: "advertising",
  scheduling_booking: "scheduling",
  calendar: "scheduling",
  entertainment_media: "entertainment",
  lifestyle_entertainment: "entertainment",
  news_lifestyle: "entertainment",
  gaming: "entertainment",
  recruiting: "hr_recruiting",
  human_resources: "hr_recruiting",
  hr_talent_recruitment: "hr_recruiting",
  education_lms: "education",
  online_courses: "education",
  drip_emails: "marketing_automation",
  transactional_email: "email",
  security_compliance: "security_identity_tools",
  cybersecurity: "security_identity_tools",
  call_tracking: "phone_sms",
  transcription: "audio_speech",
  ai_assistants: "ai_chatbots",
  media_generation: "ai_content_generation",
  webinars: "event_management",
  website_app_building: "website_builders",
  app_builder: "website_builders",
  ai_safety_compliance_detection: "ai_safety",
};

/** A category other than a curated one is listed once this many apps share it. */
const MIN_LISTED_CATEGORY = 3;

export function normalizeCategory(value: string): string {
  return value
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter((word) => word && word !== "and")
    .join("_");
}

/** Acronyms that stay upper-case in a title-cased name. */
const UPPERCASE_WORDS = new Set([
  "3d", "ai", "api", "apis", "ats", "b2b", "bi", "ci", "cms", "crm", "erp", "gis", "hr",
  "hris", "iot", "it", "kyc", "llm", "lms", "mcp", "ml", "nlp", "ocr", "pdf", "rpa",
  "saas", "sdk", "seo", "sms", "sql", "ui", "ux", "voip", "vpn",
]);
const LOWERCASE_WORDS = new Set(["a", "an", "and", "as", "at", "by", "for", "in", "of", "on", "or", "the", "to", "via", "with"]);

/** Title-cases a category name Multica has no label for. */
export function formatCategoryName(name: string): string {
  return name
    .trim()
    .split(/\s+/)
    .map((word, index) => {
      // Keep Composio's own casing ("DevOps").
      if (word !== word.toLowerCase()) return word;
      if (index > 0 && LOWERCASE_WORDS.has(word)) return word;
      const cased = word
        .split(/([-/])/)
        .map((part) => (UPPERCASE_WORDS.has(part) ? part.toUpperCase() : part))
        .join("");
      return cased.charAt(0).toUpperCase() + cased.slice(1);
    })
    .join(" ");
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
 * category, or in a small one that is not curated, go to "other".
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
    if ((group.known && CURATED.has(group.known)) || group.slugs.length >= MIN_LISTED_CATEGORY) continue;
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
