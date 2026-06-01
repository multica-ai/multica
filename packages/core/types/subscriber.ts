export interface IssueSubscriber {
  issue_id: string;
  user_type: "member" | "agent";
  user_id: string;
  reason:
    | "creator"
    | "assignee"
    | "commenter"
    | "mentioned"
    | "manual"
    // CEREBRO-PATCH(subscriber-triggered-agent): MUL-2553 — the issue was created
    // by an agent on this member's behalf (resolved via
    // agent_task_queue.original_user_id). Lets the subscriber UI explain
    // "Started on behalf of [you]" instead of just "manually following".
    | "triggered_agent";
  created_at: string;
}
