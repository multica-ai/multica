export type FeedbackKind = "bug" | "feature" | "general" | "praise";

export interface CreateFeedbackResponse {
  id: string;
  created_at: string;
}
