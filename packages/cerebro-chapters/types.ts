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

// When starting a fresh chapter, the new session either carries the previous
// chapter's handoff forward ("handoff") or opens empty ("blank"). The closing
// chapter always keeps its handoff either way.
export type ChapterStartMode = "handoff" | "blank";

export interface ChapterStartFreshInput extends ChapterHandoffInput {
  mode: ChapterStartMode;
}
