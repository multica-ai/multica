/**
 * Why someone is subscribed to an issue. `delegated` means an agent created
 * the issue on this member's behalf (MUL-5483) — they never touched it
 * themselves, so the UI has to explain the subscription, and delivery for it
 * is narrower than for the direct reasons.
 *
 * `project` means the member subscribes to the issue's project; the server
 * lists them without storing a row, and unsubscribing from the issue records an
 * opt-out for this issue only.
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
  | "delegated"
  | "project";

/**
 * How much of the issue a member subscription actually delivers, as the
 * server's notification fanout decides it. `key_updates` is the bystander
 * tier on someone else's issue (MUL-9343); `delegated` is the tier of an issue
 * an agent created on the member's behalf (MUL-5483).
 */
export type IssueSubscriberDelivery = "all" | "key_updates" | "delegated";

export interface IssueSubscriber {
  issue_id: string;
  user_type: "member" | "agent";
  user_id: string;
  reason: IssueSubscriberReason;
  created_at: string;
  /**
   * Absent on agent rows, from servers that predate it, and on rows patched
   * locally until the list is refetched. Show no tier then rather than
   * derive one: only the server knows what it delivers.
   */
  delivery?: IssueSubscriberDelivery;
}
