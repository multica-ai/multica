/**
 * Why someone is subscribed to an issue. `delegated` means an agent created
 * the issue on this member's behalf (MUL-5483) — they never touched it
 * themselves, so the UI has to explain the subscription, and delivery for it
 * is narrower than for the direct reasons.
 *
 * Server-driven and open-ended by design: the API parses `reason` as a plain
 * string, so treat an unrecognized value as a direct subscription rather than
 * dropping the row.
 */
export type IssueSubscriberReason =
  | "creator"
  | "assignee"
  | "commenter"
  | "mentioned"
  | "manual"
  | "autopilot"
  | "delegated";

export interface IssueSubscriber {
  issue_id: string;
  user_type: "member" | "agent";
  user_id: string;
  reason: IssueSubscriberReason;
  created_at: string;
}
