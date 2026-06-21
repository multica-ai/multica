export type ChapterStatus = "todo" | "in_progress" | "done";

export interface Chapter {
  id: string;
  issue_id: string;
  name: string;
  status: ChapterStatus;
  position: number;
  handoff_summary: string;
  handoff_done: string[];
  handoff_remaining: string[];
  plan_ref: string | null;
  created_at: string;
  updated_at: string;
}

export interface ChapterHandoffInput {
  summary: string;
  done: string[];
  remaining: string[];
  plan_ref?: string | null;
}
