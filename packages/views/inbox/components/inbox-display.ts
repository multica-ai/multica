import type { AgentDraftResult, InboxItem, SkillFindRecommendation } from "@multica/core/types";

function singleLine(value: string | null | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function getInboxStringDetail(item: InboxItem, key: string): string {
  const value = item.details?.[key];
  return typeof value === "string" ? value : "";
}

export function getInboxStringArrayDetail(item: InboxItem, key: string): string[] {
  const value = item.details?.[key];
  if (!Array.isArray(value)) return [];
  return value.filter((entry): entry is string => typeof entry === "string" && entry.length > 0);
}

function toSkillFindRecommendation(value: unknown): SkillFindRecommendation | null {
  if (!isRecord(value)) return null;
  const name = typeof value.name === "string" ? value.name : "";
  const description = typeof value.description === "string" ? value.description : "";
  const source_url = typeof value.source_url === "string" ? value.source_url : "";
  const reason = typeof value.reason === "string" ? value.reason : "";
  if (!name || !source_url) return null;
  return { name, description, source_url, reason };
}

export function getSkillFindRecommendations(item: InboxItem): SkillFindRecommendation[] {
  const raw = item.details?.recommendations;
  if (!Array.isArray(raw)) return [];
  return raw.map(toSkillFindRecommendation).filter((rec): rec is SkillFindRecommendation => rec !== null);
}

export function getAgentDraftResult(item: InboxItem): AgentDraftResult | null {
  const agent_id = getInboxStringDetail(item, "drafted_agent_id") || getInboxStringDetail(item, "agent_id");
  const name = getInboxStringDetail(item, "drafted_agent_name") || getInboxStringDetail(item, "name");
  const summary = getInboxStringDetail(item, "summary");
  const skill_source_urls = getInboxStringArrayDetail(item, "skill_source_urls");
  if (!agent_id || !name) return null;
  return { agent_id, name, summary, skill_source_urls };
}

export function stripQuickCreatePrefix(title: string, identifier?: string): string {
  const normalized = singleLine(title);
  if (!normalized) return "";

  if (identifier) {
    const exactPrefix = new RegExp(
      `^Created\\s+${escapeRegExp(identifier)}:\\s*`,
      "i",
    );
    const withoutExactPrefix = normalized.replace(exactPrefix, "");
    if (withoutExactPrefix !== normalized) return withoutExactPrefix.trim();
  }

  return normalized.replace(/^Created\s+[A-Z][A-Z0-9]*-\d+:\s*/i, "").trim();
}

export function getInboxDisplayTitle(item: InboxItem): string {
  if (item.type === "quick_create_done") {
    const cleanedTitle = stripQuickCreatePrefix(item.title, getInboxStringDetail(item, "identifier"));
    if (cleanedTitle) return cleanedTitle;

    const prompt = singleLine(getInboxStringDetail(item, "original_prompt"));
    if (prompt) return prompt;
  }

  if (item.type === "quick_create_failed") {
    const prompt = singleLine(getInboxStringDetail(item, "original_prompt"));
    if (prompt) return prompt;
  }

  if (item.type === "skill_find_done") {
    const prompt = singleLine(getInboxStringDetail(item, "original_prompt"));
    if (prompt) return prompt;
  }

  if (item.type === "agent_draft_done") {
    const name = singleLine(getInboxStringDetail(item, "drafted_agent_name") || getInboxStringDetail(item, "name"));
    if (name) return name;
    const prompt = singleLine(getInboxStringDetail(item, "original_prompt"));
    if (prompt) return prompt;
  }

  if (item.type === "agent_draft_failed") {
    const prompt = singleLine(getInboxStringDetail(item, "original_prompt"));
    if (prompt) return prompt;
  }

  return item.title;
}

export function getQuickCreateFailureDetail(item: InboxItem): string {
  return singleLine(getInboxStringDetail(item, "error")) || singleLine(item.body);
}
