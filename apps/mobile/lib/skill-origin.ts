/**
 * Mirrors packages/views/skills/lib/origin.ts's readOrigin — copied, not
 * imported. Mobile's package-boundary rule only whitelists @multica/core
 * (types + pure functions); packages/views is out of bounds regardless of
 * purity. See apps/mobile/CLAUDE.md "Mobile-owned updaters" for the same
 * mirror-don't-import rationale applied to realtime WS updaters.
 */
import type { SkillSummary } from "@multica/core/types";

export type SkillOriginInfo = {
  type: "runtime_local" | "clawhub" | "skills_sh" | "github" | "manual";
  provider?: string;
  runtime_id?: string;
  source_path?: string;
  source_url?: string;
};

export function readSkillOrigin(skill: SkillSummary): SkillOriginInfo {
  const raw = (skill.config?.origin ?? null) as
    | (SkillOriginInfo & Record<string, unknown>)
    | null;
  if (raw?.type === "runtime_local") return raw;
  if (raw?.type === "clawhub") return raw;
  if (raw?.type === "skills_sh") return raw;
  if (raw?.type === "github") return raw;
  return { type: "manual" };
}
