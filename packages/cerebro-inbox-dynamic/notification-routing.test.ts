import { describe, expect, it } from "vitest";
import { opensInDynamicInboxPane } from "./notification-routing";

describe("opensInDynamicInboxPane", () => {
  it.each([
    "skill_change_request_created",
    "skill_change_request_reviewed",
  ] as const)("keeps %s inside the Dynamic Inbox", (type) => {
    expect(opensInDynamicInboxPane({ type })).toBe(true);
  });

  it("leaves other non-issue notification routing unchanged", () => {
    expect(opensInDynamicInboxPane({ type: "skill_forked" })).toBe(false);
  });
});
