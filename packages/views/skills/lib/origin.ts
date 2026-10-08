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

// Hosts each hosted origin type may legitimately point at — the client-side
// mirror of the server's `detectImportSource` allowlist (skill.go). Keyed by
// origin type so a hand-edited config can't dress an arbitrary host up as a
// GitHub / Skills.sh / ClawHub link.
const ORIGIN_SOURCE_HOSTS: Partial<Record<OriginInfo["type"], readonly string[]>> = {
  github: ["github.com", "www.github.com"],
  skills_sh: ["skills.sh", "www.skills.sh"],
  clawhub: ["clawhub.ai", "www.clawhub.ai"],
};

/**
 * The origin's `source_url` validated for use as a link destination, or null.
 *
 * `origin` is persisted JSONB that anyone able to update the skill can write
 * verbatim (`PATCH /api/skills/:id` does not validate `config`), so before a
 * value becomes an `href` it must survive being treated as a destination:
 * http(s) only, and the host must match the declared origin type. Everything
 * else — manual/runtime_local origins, missing or malformed URLs, `data:` and
 * other schemes, host/type mismatches — returns null so callers fall back to
 * plain text.
 *
 * Deliberately stricter than `isRefreshableOrigin`: the server's refresh path
 * re-validates provenance itself and also accepts scheme-less values (e.g. a
 * bare ClawHub slug), which are refreshable but meaningless as an `href`.
 */
export function originSourceUrl(origin: OriginInfo | null): string | null {
  if (!origin) return null;
  const hosts = ORIGIN_SOURCE_HOSTS[origin.type];
  if (!hosts) return null;
  if (typeof origin.source_url !== "string" || origin.source_url.length === 0) {
    return null;
  }
  let parsed: URL;
  try {
    parsed = new URL(origin.source_url);
  } catch {
    return null;
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null;
  if (!hosts.includes(parsed.hostname.toLowerCase())) return null;
  return origin.source_url;
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
