import { describe, expect, it } from "vitest";
import { WORKFLOW_TEMPLATES } from "./templates";

describe("WORKFLOW_TEMPLATES", () => {
  it("ships the six startpakke entries from JEH-1047", () => {
    expect(WORKFLOW_TEMPLATES.map((t) => t.key)).toEqual([
      "status-change",
      "due-date-time",
      "all-children-done",
      "mention-agent",
      "cron",
      "sub-issue-created",
    ]);
  });

  it("has two active templates and four coming-soon placeholders", () => {
    const active = WORKFLOW_TEMPLATES.filter((t) => t.status === "active");
    const coming = WORKFLOW_TEMPLATES.filter((t) => t.status === "coming");
    expect(active).toHaveLength(2);
    expect(coming).toHaveLength(4);
  });

  it("attaches defaults to every active template (and never to coming ones)", () => {
    for (const t of WORKFLOW_TEMPLATES) {
      if (t.status === "active") {
        expect(t.defaults, `${t.key} missing defaults`).toBeDefined();
        expect(t.defaults?.name).toBeTruthy();
        expect(t.defaults?.trigger_type).toBeTruthy();
        expect(t.defaults?.action_type).toBeTruthy();
      } else {
        expect(t.defaults, `${t.key} should not have defaults yet`).toBeUndefined();
      }
    }
  });

  it("matches the status-changed → in_review acceptance scenario", () => {
    // The JEH-1047 "Done når" acceptance path: pick this template, save,
    // and a sub-issue should auto-create on transition to in_review. The
    // shape stored on the template is what the e2e test relies on, so pin
    // it.
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "status-change");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("status_changed");
    expect(
      (t!.defaults!.trigger_config as { to_status?: string }).to_status,
    ).toBe("in_review");
    expect(t!.defaults!.action_type).toBe("create_sub_issue");
  });
});
