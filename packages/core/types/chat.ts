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
}

/**
 * Response from GET /api/chat/sessions/{id}/pending-task.
 * Both fields are absent when the session has no in-flight task.
 */
export interface ChatPendingTask {
  task_id?: string;
  status?: string;
}
