// FIR-394 reminders — shared types. Mirror the backend reminderResponse
// (server/internal/cerebro/reminder/handler.go). A reminder is its own entity
// that links BACK to the source message + conversation, so reminder state can
// never again lock a DM/channel thread (FIR-249).

// Statuses the API can return on the overview. "done" is filtered server-side
// from List but can still come back from Get/Snooze/MarkDone responses.
export type ReminderStatus = "pending" | "snoozed" | "fired" | "done";

export interface Reminder {
  id: string;
  remind_at: string;
  status: ReminderStatus;
  text: string;
  // Source link — all optional: the reminder outlives the message it points at
  // (ON DELETE SET NULL on the backend).
  message_id?: string;
  conversation_id?: string;
  // issue.kind of the source conversation: "dm" / "channel" / a regular issue
  // kind. Drives whether "Gå til besked" routes to /channels or /issues.
  conversation_kind?: string;
  conversation_title?: string;
  source_preview?: string;
  fired_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateReminderInput {
  /** The comment the reminder is set on (becomes message_id server-side). */
  message_id: string;
  /** RFC3339; the server rejects a time that is not in the future. */
  remind_at: string;
  /** Optional label; the server auto-suggests from the message when empty. */
  text?: string;
}
