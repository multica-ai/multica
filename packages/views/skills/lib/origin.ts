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
 * SKILL.md is always present plus any additional attached files. Accepts a
 * `SkillSummary` because list endpoints don't return the `files` array — in
 * that case we only know the body exists, so the count falls back to 1.
 */
export function totalFileCount(skill: Skill | SkillSummary): number {
  const files = (skill as Skill).files;
  return (files?.length ?? 0) + 1;
}
