import { describe, expect, it } from "vitest";
import { WORKFLOW_TEMPLATES } from "./templates";

describe("WORKFLOW_TEMPLATES", () => {
  it("ships the eight startpakke entries (4 active + 4 coming) from JEH-1047/JEH-1103", () => {
    expect(WORKFLOW_TEMPLATES.map((t) => t.key)).toEqual([
      "status-change",
      "due-date-time",
      "run-skill",
      "comment-on-issue",
      "all-children-done",
      "mention-agent",
      "cron",
      "sub-issue-created",
    ]);
  });

  it("has four active templates and four coming-soon placeholders", () => {
    const active = WORKFLOW_TEMPLATES.filter((t) => t.status === "active");
    const coming = WORKFLOW_TEMPLATES.filter((t) => t.status === "coming");
    expect(active).toHaveLength(4);
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

  it("pins the run-skill template's status_changed → run_skill shape", () => {
    // The JEH-1103 acceptance path for phase 2: pick this template, fill in
    // skill_name + agent_id, save, and the engine should enqueue a
    // quick_create task with a synthesized prompt on transition.
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "run-skill");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("status_changed");
    expect(t!.defaults!.action_type).toBe("run_skill");
    const cfg = t!.defaults!.action_config as {
      skill_name: string;
      agent_id: string;
      skill_input: Record<string, unknown>;
    };
    // skill_name and agent_id stay blank — the user fills those in before
    // saving. The placeholder skill_input is what proves template-render
    // works against the canvas inspector.
    expect(cfg.skill_name).toBe("");
    expect(cfg.agent_id).toBe("");
    expect(cfg.skill_input).toMatchObject({ issue_title: "{{title}}" });
  });

  it("pins the comment-on-issue template's status_changed → comment shape", () => {
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "comment-on-issue");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("status_changed");
    expect(t!.defaults!.action_type).toBe("comment_on_issue");
    const cfg = t!.defaults!.action_config as {
      target: string;
      content: string;
    };
    expect(cfg.target).toBe("self");
    expect(cfg.content).toContain("{{title}}");
  });
});
