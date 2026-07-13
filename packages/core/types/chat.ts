import type { AgentTask } from "./agent";

/** A user's pinned "quick agent" for the Chat list top bar. */
export interface ChatPinnedAgent {
  agent_id: string;
  position: number;
}

/**
 * Kind of a chat message. Additive (MUL-4351): the server always sends a
 * concrete value, but treat a missing/unknown value as "message" so an older
 * server or a future kind never breaks rendering.
 * - "message"     — an ordinary user/assistant message.
 * - "no_response" — a completed direct-chat turn that produced no text reply.
 */
export type ChatMessageKind = "message" | "no_response";

/** Preview of a session's most recent message, for the IM-style list. */
export interface ChatLastMessage {
  content: string;
  role: "user" | "assistant";
  created_at: string;
  /** Present when the last message is a failed assistant reply. */
  failure_reason?: string | null;
  /** "message" (default) or "no_response". Optional for older servers. */
  message_kind?: ChatMessageKind;
}

export interface ChatSession {
  id: string;
  workspace_id: string;
  agent_id: string;
  creator_id: string;
  title: string;
  status: "active" | "archived";
  /** True when the session has any unread assistant replies. List-only.
   *  Convenience for `unread_count > 0`. */
  has_unread: boolean;
  /** Number of unread assistant messages (after the read cursor). List-only;
   *  optional so older clients / non-list payloads stay valid. */
  unread_count?: number;
  /** Latest message in the session, or null when empty. List-only. */
  last_message?: ChatLastMessage | null;
  /** True when the user has pinned this chat to the top of the list.
   *  Optional so older clients / non-list payloads stay valid. */
  pinned?: boolean;
  created_at: string;
  updated_at: string;
}

export interface PendingChatTaskItem {
  task_id: string;
  status: string;
  chat_session_id: string;
}

export interface PendingChatTasksResponse {
  tasks: PendingChatTaskItem[];
}

/**
 * Boolean fast-path payload for the FAB "running" indicator — returned by
 * GET /api/chat/pending-tasks/has-any. The FAB only needs to know whether any
 * in-flight chat task exists, so it avoids fetching the full task list.
 */
export interface HasPendingChatTasksResponse {
  has_pending: boolean;
}

export interface ChatMessage {
  id: string;
  chat_session_id: string;
  role: "user" | "assistant";
  content: string;
  task_id: string | null;
  created_at: string;
  /**
   * Attachments linked to this message via the attachment table's
   * chat_message_id FK. Populated by ListChatMessages. UI renders these
   * as file/image cards inside the bubble; the markdown URL inline in
   * `content` may have an expiring signature, while attachment metadata
   * here is stable and the source of truth for click-time download.
   */
  attachments?: import("./attachment").Attachment[];
  /**
   * When set, this is an assistant message synthesized by the server's
   * FailTask fallback (mirrors the issue path's failure system comment).
   * `content` carries the raw daemon-reported errMsg; the front-end maps
   * `failure_reason` (an enum like "agent_error" / "connection_error" /
   * "timeout") to a user-facing label and renders a destructive bubble.
   * Null on success messages and on user messages.
   */
  failure_reason?: string | null;
  /**
   * Wall-clock duration from `task.created_at` (user hit send) to terminal
   * state (completed/failed). Set by the server on assistant messages
   * synthesized by CompleteTask/FailTask. UI renders it as "Replied in
   * 38s" / "Failed after 12s" beneath the bubble. Null on user messages
   * and on legacy assistant messages predating migration 063.
   */
  elapsed_ms?: number | null;
  /**
   * "message" (default) or "no_response" — a completed direct-chat turn that
   * produced no text reply (MUL-4351). Optional/additive: absent on older
   * servers and on user messages; treat a missing value as "message".
   */
  message_kind?: ChatMessageKind;
}

export interface ChatMessagesCursor {
  created_at: string;
  id: string;
}

export interface ChatMessagesPage {
  messages: ChatMessage[];
  limit: number;
  has_more: boolean;
  next_cursor?: ChatMessagesCursor | null;
}

export interface SendChatMessageResponse {
  message_id: string;
  task_id: string;
  /**
   * Server-authoritative task creation time. Optimistic StatusPill seed
   * uses this as its anchor so the timer starts from the real `0s` —
   * without it the front-end falls back to its local clock and the
   * timer "snaps backwards" later when WS events update the cache.
   */
  created_at: string;
  /**
   * Attachment ids the server actually bound to this message. The client
   * diffs these against the ids it requested to warn when an attachment
   * silently failed to bind — no extra fetch needed. Optional for forward
   * compat with servers that predate the field.
   */
  attachment_ids?: string[];
}

export interface CancelledChatMessage {
  chat_session_id: string;
  message_id: string;
  content: string;
  restore_to_input: boolean;
  /**
   * Attachments detached from the deleted message so a restored draft can
   * re-bind them on re-send. Absent on servers that predate the field.
   */
  attachments?: import("./attachment").Attachment[];
}

export interface CancelTaskResponse extends AgentTask {
  cancelled_chat_message?: CancelledChatMessage;
}

/**
 * Response from GET /api/chat/sessions/{id}/pending-task.
 * All fields are absent when the session has no in-flight task.
 *
 * `created_at` is the server-authoritative anchor for the chat StatusPill's
 * elapsed-seconds timer — the optimistic seed in chat-window.tsx fills in
 * task_id/status only, then this query catches up with the real created_at
 * so the timer survives refresh / reopen without "resetting to 0s".
 */
export interface ChatPendingTask {
  task_id?: string;
  status?: string;
  created_at?: string;
}
