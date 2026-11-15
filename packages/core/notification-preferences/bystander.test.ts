import { describe, expect, it } from "vitest";
import { receivesKeyUpdatesOnly } from "./bystander";

const me = "user-me";
const agents = [
  { id: "agent-mine", owner_id: me },
  { id: "agent-theirs", owner_id: "user-other" },
];

function check(
  reason: Parameters<typeof receivesKeyUpdatesOnly>[0]["reason"],
  issue: Parameters<typeof receivesKeyUpdatesOnly>[0]["issue"],
  preferences = {},
) {
  return receivesKeyUpdatesOnly({ reason, issue, userId: me, agents, preferences });
}

describe("receivesKeyUpdatesOnly", () => {
  it("narrows a passive subscription to someone else's issue", () => {
    expect(check("commenter", { assignee_type: "member", assignee_id: "user-other" })).toBe(true);
    expect(check("creator", { assignee_type: "agent", assignee_id: "agent-theirs" })).toBe(true);
    expect(check("mentioned", { assignee_type: "squad", assignee_id: "squad-1" })).toBe(true);
  });

  it("keeps full delivery on the member's own issue or an unassigned one", () => {
    expect(check("commenter", { assignee_type: "member", assignee_id: me })).toBe(false);
    expect(check("creator", { assignee_type: "agent", assignee_id: "agent-mine" })).toBe(false);
    expect(check("creator", { assignee_type: null, assignee_id: null })).toBe(false);
  });

  it("keeps full delivery for explicit subscriptions and the all preference", () => {
    const theirs = { assignee_type: "member", assignee_id: "user-other" } as const;
    expect(check("manual", theirs)).toBe(false);
    expect(check("autopilot", theirs)).toBe(false);
    expect(check("delegated", theirs)).toBe(false);
    expect(check("project", theirs)).toBe(false);
    expect(check("commenter", theirs, { others_issues: "all" })).toBe(false);
  });

  it("keeps full delivery when the member also watches the project", () => {
    expect(
      receivesKeyUpdatesOnly({
        reason: "commenter",
        projectCovered: true,
        issue: { assignee_type: "member", assignee_id: "user-other" },
        userId: me,
        agents,
        preferences: {},
      }),
    ).toBe(false);
  });
});
