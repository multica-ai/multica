import { z } from "zod";

/**
 * A member's rating of a brief (spec 2.8). `POST /api/briefs/{id}/feedback`
 * replaces the member's earlier rating, tags and note. The summary is the
 * workspace's rolling usefulness, readable by owners and admins only.
 */
export const BRIEF_FEEDBACK_RATINGS = ["useful", "not_useful"] as const;
export type BriefFeedbackRating = (typeof BRIEF_FEEDBACK_RATINGS)[number];

export const BRIEF_FEEDBACK_TAGS = [
  "too_long",
  "missing_something",
  "wrong",
  "too_vague",
  "not_for_me",
] as const;
export type BriefFeedbackTag = (typeof BRIEF_FEEDBACK_TAGS)[number];

export function isBriefFeedbackTag(value: string): value is BriefFeedbackTag {
  return (BRIEF_FEEDBACK_TAGS as readonly string[]).includes(value);
}

export interface BriefFeedback {
  id: string;
  brief_id: string;
  rating: BriefFeedbackRating;
  tags: BriefFeedbackTag[];
  note: string;
  created_at: string;
  updated_at: string;
}

export interface SubmitBriefFeedbackRequest {
  rating: BriefFeedbackRating;
  tags: BriefFeedbackTag[];
  note: string;
}

export interface BriefFeedbackSummary {
  /** The window in days; the server uses 14. */
  days: number;
  ratings: number;
  useful: number;
  /** Share of useful ratings, 0 to 1. Null for an empty window, so it does not read as 0%. */
  usefulness: number | null;
}

// A rating the client does not know is not a rating to show: the whole feedback
// reads as none. Unknown tags are dropped, since the dialog offers only the known ones.
export const BriefFeedbackSchema = z
  .object({
    id: z.string().default(""),
    brief_id: z.string().default(""),
    rating: z.enum(BRIEF_FEEDBACK_RATINGS),
    tags: z
      .array(z.string())
      .nullable()
      .default([])
      .transform((tags) => (tags ?? []).filter(isBriefFeedbackTag)),
    note: z.string().default(""),
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
  })
  .loose();

export const BriefFeedbackOrNoneSchema = BriefFeedbackSchema.nullable();

export const BriefFeedbackSummarySchema = z
  .object({
    days: z.number().default(14),
    ratings: z.number().default(0),
    useful: z.number().default(0),
    usefulness: z.number().min(0).max(1).nullable().catch(null).default(null),
  })
  .loose();
