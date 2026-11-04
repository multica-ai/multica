export type ConversationLocale = "en" | "zh-CN";

/** One row of the transcript, as the turn derives it. */
export interface ConversationMessage {
  messageId: string;
  turnId: string | null;
  role: "user" | "assistant";
  kind: string;
  status?: string;
  semantics?: Record<string, unknown>;
  occurredAtUnixMs: number;
  payload: Record<string, unknown>;
}
