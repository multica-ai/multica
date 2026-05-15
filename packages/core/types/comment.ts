export type CommentType = "comment" | "status_change" | "progress_update" | "system";

export type CommentAuthorType = "member" | "agent";

export interface Reaction {
  id: string;
  comment_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

export interface Comment {
  id: string;
  issue_id: string;
  author_type: CommentAuthorType;
  author_id: string;
  content: string;
  type: CommentType;
  parent_id: string | null;
  reactions: Reaction[];
  attachments: import("./attachment").Attachment[];
  created_at: string;
  updated_at: string;
  resolved_at: string | null;
  resolved_by_type: CommentAuthorType | null;
  resolved_by_id: string | null;
}

// CEREBRO-PATCH(comments-move-to-subissue-ui): JEH-1309 frontend response for moving a thread to a sub-issue.
export interface MoveCommentToSubIssueResponse {
  issue_id: string;
  identifier: string;
  number: number;
}
