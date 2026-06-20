// FIR-394 reminders — shared types. Mirror the backend reminderResponse
// (server/internal/cerebro/reminder/handler.go). A reminder is its own entity
// that links BACK to the source message + conversation, so reminder state can
// never again lock a DM/channel thread (FIR-249).

// Statuses the API can return on the overview. "done" is filtered server-side
// from List but can still come back from Get/Snooze/MarkDone responses.
export type ReminderStatus = "pending" | "snoozed" | "fired" | "done";

// Who the reminder is for. A "member" lands in that person's inbox; an "agent"
// fires a wakeup (a scheduled agent run).
export type RecipientType = "member" | "agent";

// What the reminder points at. "comment" = a DM/channel/issue message;
// "issue" = an issue itself; "project" = a project; "chat_message" = a specific
// message in an agent chat; "none" = a free reminder carried entirely by text.
export type AnchorType = "comment" | "issue" | "project" | "chat_message" | "none";

export interface Reminder {
  id: string;
  creator_id: string;
  recipient_type: RecipientType;
  recipient_id: string;
  remind_at: string;
  status: ReminderStatus;
  text: string;
  anchor_type: AnchorType;
  // Anchor links — all optional: the reminder outlives whatever it points at
  // (ON DELETE SET NULL on the backend).
  message_id?: string;
  conversation_id?: string;
  // issue.kind of the source conversation: "dm" / "channel" / a regular issue
  // kind. Drives whether "Go to message" routes to /channels or /issues.
  conversation_kind?: string;
  conversation_title?: string;
  project_id?: string;
  project_title?: string;
  // Chat-message anchor: the specific chat message + its session (so "Go to
  // chat" re-opens the right chat) + the chat title for the anchor label.
  chat_message_id?: string;
  chat_session_id?: string;
  chat_session_title?: string;
  source_preview?: string;
  fired_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateReminderInput {
  /** RFC3339; the server rejects a time that is not in the future. */
  remind_at: string;
  /** Optional label; the server auto-suggests from the message when empty, and
   *  requires it when there is no anchor. */
  text?: string;
  /** Recipient — defaults to the caller as a member when omitted. An agent
   *  recipient requires recipient_id and a comment/issue anchor. */
  recipient_type?: RecipientType;
  recipient_id?: string;
  // Anchor — set at most one. None set ⇒ a free reminder (text required).
  /** The comment the reminder is set on (becomes message_id server-side). */
  message_id?: string;
  /** An issue the reminder points at. */
  issue_id?: string;
  /** A project the reminder points at. */
  project_id?: string;
  /** A specific chat message the reminder points at. */
  chat_message_id?: string;
}
