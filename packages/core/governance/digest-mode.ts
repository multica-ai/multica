import { z } from "zod";
import { parseWithFallback } from "../api/schema";

// The Secretary's digest source (spec 4.8). auto reads floor briefs above a squad
// count; squads and floors pin the source.
export const DIGEST_MODES = ["auto", "squads", "floors"] as const;
export type DigestSetting = (typeof DIGEST_MODES)[number];

export interface DigestModeInfo {
  setting: DigestSetting;
  active_squads: number;
  resolved: "squads" | "floors";
  auto_floor_threshold: number;
}

const digestModeSchema = z.object({
  setting: z.enum(DIGEST_MODES).catch("auto").default("auto"),
  active_squads: z.number().int().nonnegative().catch(0).default(0),
  resolved: z.enum(["squads", "floors"]).catch("squads").default("squads"),
  auto_floor_threshold: z.number().int().nonnegative().catch(30).default(30),
});

export const FALLBACK_DIGEST_MODE: DigestModeInfo = {
  setting: "auto",
  active_squads: 0,
  resolved: "squads",
  auto_floor_threshold: 30,
};

export function parseDigestMode(raw: unknown): DigestModeInfo {
  return parseWithFallback<DigestModeInfo>(raw, digestModeSchema, FALLBACK_DIGEST_MODE, {
    endpoint: "GET /api/office/digest-mode",
  });
}
