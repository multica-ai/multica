import { describe, expect, it } from "vitest";
import { WORKFLOW_TEMPLATES } from "./templates";

describe("WORKFLOW_TEMPLATES", () => {
  it("ships the nine startpakke entries (5 active + 4 coming) from JEH-1047/JEH-1103/JEH-1114", () => {
    expect(WORKFLOW_TEMPLATES.map((t) => t.key)).toEqual([
      "status-change",
      "due-date-time",
      "run-skill",
      "comment-on-issue",
      "route-by-domain",
      "all-children-done",
      "mention-agent",
      "cron",
      "sub-issue-created",
    ]);
  });

  it("has five active templates and four coming-soon placeholders", () => {
    const active = WORKFLOW_TEMPLATES.filter((t) => t.status === "active");
    const coming = WORKFLOW_TEMPLATES.filter((t) => t.status === "coming");
    expect(active).toHaveLength(5);
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

  it("pins the route-by-domain template's classifier shape", () => {
    // JEH-1114 phase-2 ext PR 1: pick this template, save (workspace must
    // already have `domain:code|business|design|content` labels), and the
    // engine attaches `domain:<x>` based on the issue title + description.
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "route-by-domain");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("status_changed");
    expect(t!.defaults!.action_type).toBe("route_by_domain");
    const cfg = t!.defaults!.action_config as {
      label_prefix: string;
      default_domain: string;
    };
    expect(cfg.label_prefix).toBe("domain:");
    expect(cfg.default_domain).toBe("business");
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
