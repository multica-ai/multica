export type EpicStatus = "open" | "closed";

export interface Epic {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  color: string;
  status: EpicStatus;
  issue_count: number;
  done_count: number;
  created_at: string;
  updated_at: string;
}
