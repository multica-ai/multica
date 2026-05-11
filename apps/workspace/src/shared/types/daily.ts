// Type definitions for daily review and daily plan features.

export interface DailyReview {
  id: string;
  workspace_id: string;
  user_id: string;
  /** ISO date string: YYYY-MM-DD */
  review_date: string;
  draft_content: string;
  status: "draft" | "confirmed";
  confirmed_at: string | null;
  generated_by: string;
  created_at: string;
  updated_at: string;
}

export interface DailyPlan {
  id: string;
  workspace_id: string;
  user_id: string;
  /** ISO date string: YYYY-MM-DD */
  plan_date: string;
  draft_content: string;
  top_issue_ids: string[];
  status: "draft" | "confirmed";
  confirmed_at: string | null;
  generated_by: string;
  created_at: string;
  updated_at: string;
}
