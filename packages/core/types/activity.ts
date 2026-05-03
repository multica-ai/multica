import type { Reaction } from "./comment";
import type { Attachment } from "./attachment";

export interface AssigneeFrequencyEntry {
  assignee_type: string;
  assignee_id: string;
  frequency: number;
}

export interface CommitFileChange {
  path: string;
  additions: number;
  deletions: number;
  status: "added" | "modified" | "deleted" | "renamed";
}

export interface CommitDetails {
  sha: string;
  short_sha: string;
  message: string;
  url?: string;
  branch?: string;
  repo?: string;
  author_name?: string;
  author_email?: string;
  committed_at?: string;
  files?: CommitFileChange[];
  total_additions?: number;
  total_deletions?: number;
  total_files?: number;
  diff?: string;
}

export interface TimelineEntry {
  type: "activity" | "comment";
  id: string;
  actor_type: string;
  actor_id: string;
  created_at: string;
  // Activity fields
  action?: string;
  details?: Record<string, unknown>;
  // Comment fields
  content?: string;
  parent_id?: string | null;
  updated_at?: string;
  comment_type?: string;
  reactions?: Reaction[];
  attachments?: Attachment[];
}
