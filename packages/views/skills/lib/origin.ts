import type { Skill, SkillSummary } from "@multica/core/types";

/**
 * Discriminated view over `Skill.config.origin` — the JSONB blob the backend
 * writes when a skill was imported from outside (local runtime, ClawHub,
 * Skills.sh, GitHub). Manual creates have no origin, so we synthesize
 * `{ type: "manual" }` for them to keep the consumer code uniform.
 */
export type OriginInfo = {
  type: "runtime_local" | "clawhub" | "skills_sh" | "github" | "manual";
  provider?: string;
  runtime_id?: string;
  source_path?: string;
  source_url?: string;
  owner?: string;
  repo?: string;
  ref?: string;
  path?: string;
  skill?: string;
  slug?: string;
};

export function readOrigin(skill: SkillSummary): OriginInfo {
  const raw = (skill.config?.origin ?? null) as
    | (OriginInfo & Record<string, unknown>)
    | null;
  if (raw?.type === "runtime_local") return raw;
  if (raw?.type === "clawhub") return raw;
  if (raw?.type === "skills_sh") return raw;
  if (raw?.type === "github") return raw;
  return { type: "manual" };
}

/**
 * Whether the skill can be re-downloaded from where it was imported. Only
 * hosted sources qualify: runtime-local copies re-import through the daemon,
 * and manual / archive-uploaded skills have no upstream at all. The server
 * enforces the same rule on `POST /api/skills/:id/refresh`.
 */
export function isRefreshableOrigin(origin: OriginInfo): boolean {
  return (
    (origin.type === "github" || origin.type === "skills_sh" || origin.type === "clawhub") &&
    typeof origin.source_url === "string" &&
    origin.source_url.length > 0
  );
}

/**
 * SKILL.md is always present plus any additional attached files. Accepts a
 * `SkillSummary` because list endpoints don't return the `files` array — in
 * that case we only know the body exists, so the count falls back to 1.
 */
export function totalFileCount(skill: Skill | SkillSummary): number {
  const files = (skill as Skill).files;
  return (files?.length ?? 0) + 1;
}
