import { describe, expect, it } from "vitest";
import { isOwnIssue } from "./ownership";

const agents = [
  { id: "agent-mine", owner_id: "me" },
  { id: "agent-theirs", owner_id: "someone" },
];

describe("isOwnIssue", () => {
  it("is the viewer's when assigned to them or to an agent they own", () => {
    expect(isOwnIssue({ assignee_type: "member", assignee_id: "me" }, "me", agents)).toBe(true);
    expect(isOwnIssue({ assignee_type: "agent", assignee_id: "agent-mine" }, "me", agents)).toBe(true);
  });

  it("is someone else's when assigned elsewhere, to a squad, or to nobody", () => {
    expect(isOwnIssue({ assignee_type: "member", assignee_id: "someone" }, "me", agents)).toBe(false);
    expect(isOwnIssue({ assignee_type: "agent", assignee_id: "agent-theirs" }, "me", agents)).toBe(false);
    expect(isOwnIssue({ assignee_type: "squad", assignee_id: "squad-1" }, "me", agents)).toBe(false);
    expect(isOwnIssue({ assignee_type: null, assignee_id: null }, "me", agents)).toBe(false);
  });

  it("is never the viewer's without a viewer", () => {
    expect(isOwnIssue({ assignee_type: "member", assignee_id: "me" }, null, agents)).toBe(false);
  });
});
