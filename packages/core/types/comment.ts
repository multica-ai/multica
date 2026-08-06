export type CommentType = "comment" | "status_change" | "progress_update" | "system" | "ask_user_question";

// `system` is used by platform-generated rows (e.g. the parent-issue
// child-done notification, MUL-2538). System rows carry a zero UUID for
// author_id; render paths should branch on author_type rather than the UUID.
export type CommentAuthorType = "member" | "agent" | "system";

/** One selectable choice in an ask_user_question comment. `label` is the short
 *  headline (rendered on top), `description` the longer explanation (below). */
export interface AskUserQuestionOption {
  label: string;
  description: string;
}

/** Written once the target user responds. Absent until then. Selection is
 *  recorded per mode: `selected_index` (single, also legacy rows),
 *  `selected_indices` (multi), `custom_text` (the auto "Other" free-text). */
export interface AskUserQuestionAnswer {
  state: "submitted" | "ignored";
  selected_index?: number;
  selected_indices?: number[];
  custom_text?: string;
  answered_at: string;
}

/** Structured payload for an ask_user_question comment, stored under
 *  comment.metadata.ask_user_question. */
export interface AskUserQuestionMeta {
  /** user_id of the human being asked; only this user may answer. */
  target_user: string;
  /** agent id that asked (= comment author); the confirmation reply @mentions it. */
  source_user: string;
  question: string;
  options: AskUserQuestionOption[];
  /** Allow picking multiple options (checkbox). Default false = single (radio). */
  multi_select?: boolean;
  /** Append an auto "Other" choice with a free-text input. Default false. */
  allow_custom?: boolean;
  answer?: AskUserQuestionAnswer | null;
}

/** Decoded comment.metadata. Only ask_user_question is modeled today. */
export interface CommentMetadata {
  ask_user_question?: AskUserQuestionMeta;
}

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
  metadata?: CommentMetadata | null;
  reactions: Reaction[];
  attachments: import("./attachment").Attachment[];
  created_at: string;
  updated_at: string;
  resolved_at: string | null;
  resolved_by_type: CommentAuthorType | null;
  resolved_by_id: string | null;
  source_task_id?: string | null;
  // The quick action that produced this comment (MUL-5465). A quick action
  // posts an ORDINARY comment and marks it with this id; the collapsed card
  // keys off the id rather than a dedicated `type`, because `type` is
  // client-supplied on the generic comment endpoint and would be forgeable.
  quick_action_id?: string | null;
  // Per-target result of every explicit @agent / @squad mention in this comment
  // (MUL-4525 §2). Present only on create/edit responses; older servers omit it.
  trigger_outcomes?: CommentTriggerOutcome[];
}

// The domain result of one explicitly-mentioned trigger target. Success-shaped
// statuses (queued/coalesced/deferred) mean the mention was handled; `blocked`
// means it was refused with an enumeration-safe reason_code.
export type CommentTriggerStatus =
  | "queued"
  | "coalesced"
  | "deferred"
  | "blocked";

export interface CommentTriggerOutcome {
  target_type: string; // "agent" | "squad"
  target_id: string;
  status: CommentTriggerStatus | string;
  reason_code: string;
}

export type CommentTriggerSource =
  | "issue_assignee"
  | "mention_agent"
  | "mention_squad_leader";

export interface CommentTriggerPreviewAgent {
  id: string;
  name: string;
  avatar_url?: string;
  source: CommentTriggerSource | string;
  reason: string;
}

export interface CommentTriggerPreview {
  agents: CommentTriggerPreviewAgent[];
  // Explicit @agent / @squad mentions that will NOT trigger if posted as-is
  // (MUL-4525 §2). Additive: older servers omit it.
  blocked?: CommentTriggerOutcome[];
}
