export interface ChatSession {
  id: string;
  workspace_id: string;
  agent_id: string;
  creator_id: string;
  title: string;
  status: "active" | "archived";
  /** True when the session has any unread assistant replies. List-only. */
  has_unread: boolean;
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

export interface ChatMessage {
  id: string;
  chat_session_id: string;
  role: "user" | "assistant";
  content: string;
  task_id: string | null;
  created_at: string;
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
   * RFC3339 timestamp set when an assistant turn has been written for this
   * user message. `null` on a user row means "still waiting for the agent"
   * — the queue/in-flight indicator hangs off this. Always null on
   * assistant rows.
   */
  responded_at: string | null;
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

/**
 * Aggregate token + cost spend for a chat session. Mirrors IssueUsageSummary
 * so the chat session header can show the same "Session price + token
 * breakdown" the issue sidebar shows. Cost cents come from pkg/pricing on
 * the server.
 */
export interface ChatSessionUsage {
  total_input_tokens: number;
  total_output_tokens: number;
  total_cache_read_tokens: number;
  total_cache_write_tokens: number;
  task_count: number;
  cost_cents: number;
}
