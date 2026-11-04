import { z } from "zod";
import { parseWithFallback } from "../api/schema";

/**
 * Squad autonomy levels (spec 4.3). L0 Suggest holds routine runs; L1 Work is
 * the default; L2 Trusted lets knowledge drafts publish after review and lets
 * standing approvals apply; L3 Autonomous lets a standing approval create
 * initiatives at the first stage. Launches and organisation changes stay the
 * boss's at every level.
 */
export const AUTONOMY_LEVELS = [0, 1, 2, 3] as const;
export type AutonomyLevel = (typeof AUTONOMY_LEVELS)[number];

/** A squad's level and the reason and time of its last change. */
export interface SquadAutonomy {
  squad_id: string;
  /** The level as the server sent it. A level this client does not know still shows, by number. */
  level: number;
  level_name: string;
  /** Why the level was last changed. Empty while the level is the default. */
  reason: string;
  /** When the level was last changed, or null while it is the default. */
  changed_at: string | null;
}

export interface SetSquadAutonomyRequest {
  level: AutonomyLevel;
  /** Required: the reason stays in the governance log. */
  reason: string;
}

/** The default level of a squad that has never been changed (spec 4.3). */
export const DEFAULT_AUTONOMY_LEVEL = 1;

const autonomyNames: Record<number, string> = { 0: "Suggest", 1: "Work", 2: "Trusted", 3: "Autonomous" };

/** The name a level shows as. A level the client does not know has no name. */
export function autonomyLevelName(level: number): string {
  return autonomyNames[level] ?? "";
}

export const SquadAutonomySchema = z
  .object({
    squad_id: z.string().default(""),
    level: z.number().int().default(DEFAULT_AUTONOMY_LEVEL),
    level_name: z.string().nullish().transform((name) => name ?? ""),
    reason: z.string().nullish().transform((reason) => reason ?? ""),
    changed_at: z.string().nullish().transform((at) => at ?? null),
  })
  .loose()
  .transform(
    (a): SquadAutonomy => ({
      ...a,
      level_name: a.level_name || autonomyLevelName(a.level),
    }),
  );

export const FALLBACK_SQUAD_AUTONOMY: SquadAutonomy = {
  squad_id: "",
  level: DEFAULT_AUTONOMY_LEVEL,
  level_name: autonomyLevelName(DEFAULT_AUTONOMY_LEVEL),
  reason: "",
  changed_at: null,
};

/** Reads a squad's autonomy response; a malformed one falls back to the default level. */
export function parseSquadAutonomy(raw: unknown, endpoint: string): SquadAutonomy {
  return parseWithFallback<SquadAutonomy>(raw, SquadAutonomySchema, FALLBACK_SQUAD_AUTONOMY, { endpoint });
}
