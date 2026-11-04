import { z } from "zod";

// Scorecards and reviews (spec 4.6): types and lenient schemas, kept free of
// the api client so the client can parse with them. The schemas are lenient on
// purpose: a missing field takes its default and an unknown rating reads as
// "unknown", so one odd row never blanks the tab (see parseWithFallback in
// api/schema.ts).

export const REVIEW_RATINGS = ["exceeds", "meets", "below", "at_risk"] as const;
export type ReviewRating = (typeof REVIEW_RATINGS)[number] | "unknown";

export const SCORECARD_SUBJECT_TYPES = ["agent", "squad", "company"] as const;
export type ScorecardSubjectType = (typeof SCORECARD_SUBJECT_TYPES)[number];

export interface ScorecardMetric {
  /** null when the metric has no data in the period. */
  value: number | null;
  sample: number;
}

export interface Scorecard {
  id: string;
  subject_type: ScorecardSubjectType | "unknown";
  subject_id: string;
  period: "week" | "month" | "unknown";
  period_start: string;
  period_end: string;
  status: "ok" | "insufficient" | "unknown";
  sample_size: number;
  computed_at: string;
  metrics: Record<string, ScorecardMetric>;
  failures_by_cause: Record<string, number>;
}

export interface ReviewRecommendation {
  type: string;
  rationale: string;
  proposal_id: string | null;
  proposal_issue_id: string | null;
  refused: string | null;
}

export interface Review {
  id: string;
  subject_type: ScorecardSubjectType | "unknown";
  subject_id: string;
  period_start: string;
  period_end: string;
  scorecard_id: string;
  rating: ReviewRating;
  narrative: string;
  recommendations: ReviewRecommendation[];
  reviewer_agent_id: string;
  created_at: string;
}

export interface ReviewFilters {
  subject_type?: ScorecardSubjectType;
  subject_id?: string;
}

const subjectTypeSchema = z
  .string()
  .default("unknown")
  .transform((t): ScorecardSubjectType | "unknown" =>
    (SCORECARD_SUBJECT_TYPES as readonly string[]).includes(t) ? (t as ScorecardSubjectType) : "unknown",
  );

const ratingSchema = z
  .string()
  .default("unknown")
  .transform((r): ReviewRating =>
    (REVIEW_RATINGS as readonly string[]).includes(r) ? (r as ReviewRating) : "unknown",
  );

const metricSchema = z.object({
  value: z.number().nullable().default(null),
  sample: z.number().default(0),
});

const scorecardSchema = z.object({
  id: z.string().default(""),
  subject_type: subjectTypeSchema,
  subject_id: z.string().default(""),
  period: z.enum(["week", "month"]).or(z.string()).default("unknown").transform((p) =>
    p === "week" || p === "month" ? p : "unknown",
  ),
  period_start: z.string().default(""),
  period_end: z.string().default(""),
  status: z.enum(["ok", "insufficient"]).or(z.string()).default("unknown").transform((s) =>
    s === "ok" || s === "insufficient" ? s : "unknown",
  ),
  sample_size: z.number().default(0),
  computed_at: z.string().default(""),
  metrics: z.record(z.string(), metricSchema).default({}),
  failures_by_cause: z.record(z.string(), z.number()).default({}),
});

const recommendationSchema = z.object({
  type: z.string().default("observe"),
  rationale: z.string().default(""),
  proposal_id: z.string().nullable().default(null),
  proposal_issue_id: z.string().nullable().default(null),
  refused: z.string().nullable().default(null),
});

const reviewSchema = z.object({
  id: z.string().default(""),
  subject_type: subjectTypeSchema,
  subject_id: z.string().default(""),
  period_start: z.string().default(""),
  period_end: z.string().default(""),
  scorecard_id: z.string().default(""),
  rating: ratingSchema,
  narrative: z.string().default(""),
  recommendations: z.array(recommendationSchema).default([]),
  reviewer_agent_id: z.string().default(""),
  created_at: z.string().default(""),
});

export const ScorecardListSchema = z.object({
  scorecards: z.array(scorecardSchema).default([]),
});

export const ReviewListSchema = z.object({
  reviews: z.array(reviewSchema).default([]),
});

export const EMPTY_SCORECARD_LIST: { scorecards: Scorecard[] } = { scorecards: [] };
export const EMPTY_REVIEW_LIST: { reviews: Review[] } = { reviews: [] };

