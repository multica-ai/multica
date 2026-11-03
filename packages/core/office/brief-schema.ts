import { z } from "zod";

/**
 * A brief is the Secretary's written report to the boss for a period. `unknown`
 * stands in for a kind a newer server may add: views render it as a plain
 * brief.
 */
export type BriefKind = "daily" | "weekly" | "ad_hoc" | "unknown";
export type PublishableBriefKind = Exclude<BriefKind, "unknown">;

const KNOWN_KINDS = ["daily", "weekly", "ad_hoc"] as const;

export interface Brief {
  id: string;
  workspace_id: string;
  kind: BriefKind;
  period_start: string;
  period_end: string;
  title: string;
  /**
   * Markdown with the H2 sections in spec 3.7 order: Headline, Needs your decision, Progress,
   * Pipeline, Goals, Budget, Blockers and risks, Watch, People, FYI. The first three, Blockers
   * and risks and FYI are required; the rest appear when there is data.
   */
  body: string;
  /** Proposals the brief raises, to render with Approve and Reject beneath the text. */
  proposal_ids: string[];
  /** `agent` (the Secretary) or `member` (an admin publishing by hand). */
  created_by_type: string;
  created_by_id: string;
  created_at: string;
}

export interface PublishBriefRequest {
  kind: PublishableBriefKind;
  title: string;
  body: string;
  proposal_ids?: string[];
  /** RFC 3339. The server defaults the period from the kind. */
  period_start?: string;
  period_end?: string;
}

export interface ListBriefsParams {
  kind?: PublishableBriefKind;
  /** 1 to 50; the server defaults to 30. */
  limit?: number;
}

const BriefKindSchema = z
  .string()
  .default("unknown")
  .transform((s): BriefKind => ((KNOWN_KINDS as readonly string[]).includes(s) ? (s as BriefKind) : "unknown"));

export const BriefSchema = z
  .object({
    id: z.string().default(""),
    workspace_id: z.string().default(""),
    kind: BriefKindSchema,
    period_start: z.string().default(""),
    period_end: z.string().default(""),
    title: z.string().default(""),
    body: z.string().default(""),
    proposal_ids: z.array(z.string()).nullable().default([]).transform((v) => v ?? []),
    created_by_type: z.string().default(""),
    created_by_id: z.string().default(""),
    created_at: z.string().default(""),
  })
  .loose();

export const BriefListSchema = z.array(BriefSchema);

/** What an unreadable single-brief response degrades to: an id-less, empty brief. */
export const FALLBACK_BRIEF: Brief = {
  id: "",
  workspace_id: "",
  kind: "unknown",
  period_start: "",
  period_end: "",
  title: "",
  body: "",
  proposal_ids: [],
  created_by_type: "",
  created_by_id: "",
  created_at: "",
};
