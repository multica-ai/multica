export const FEEDBACK_KINDS = ["bug", "feature", "general", "praise"] as const;

export type FeedbackKind = (typeof FEEDBACK_KINDS)[number];

export interface CreateFeedbackResponse {
  id: string;
  created_at: string;
}
