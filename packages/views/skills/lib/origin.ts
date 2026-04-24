import type { Skill } from "@multica/core/types";

/**
 * Discriminated view over `Skill.config.origin` — the JSONB blob the backend
 * writes when a skill was imported from outside (local runtime, ClawHub,
 * Skills.sh). Manual creates have no origin, so we synthesize `{ type:
 * "manual" }` for them to keep the consumer code uniform.
 *
 * NOTE: the backend currently only writes `runtime_local` origins. URL
 * imports leave `config.origin` empty, so `clawhub`/`skills_sh` variants are
 * declared here for forward compatibility but should never be rendered in
 * the UI until the server fills them in.
 */
export type OriginInfo = {
  type: "runtime_local" | "clawhub" | "skills_sh" | "manual";
  provider?: string;
  runtime_id?: string;
  source_path?: string;
  source_url?: string;
};

export function readOrigin(skill: Skill): OriginInfo {
  const raw = (skill.config?.origin ?? null) as
    | (OriginInfo & Record<string, unknown>)
    | null;
  if (raw?.type === "runtime_local") return raw;
  if (raw?.type === "clawhub") return raw;
  if (raw?.type === "skills_sh") return raw;
  return { type: "manual" };
}

/** SKILL.md is always present plus any additional attached files. */
export function totalFileCount(skill: Skill): number {
  return (skill.files?.length ?? 0) + 1;
}
