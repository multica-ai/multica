import { describe, expect, it } from "vitest";
import { WORKFLOW_TEMPLATES } from "./templates";

describe("WORKFLOW_TEMPLATES", () => {
  it("ships the thirteen startpakke entries (all active after phase 3) from JEH-1047/JEH-1103/JEH-1114/JEH-1108", () => {
    expect(WORKFLOW_TEMPLATES.map((t) => t.key)).toEqual([
      "status-change",
      "due-date-time",
      "run-skill",
      "comment-on-issue",
      "route-by-domain",
      "validate-evidence-on-review",
      "escalate-stalled-to-owner",
      "all-children-done",
      "mention-agent",
      "cron",
      "sub-issue-created",
      "cron-daily-standup",
      "webhook-github-pr",
    ]);
  });

  it("has thirteen active templates and zero remaining placeholders after phase 3", () => {
    const active = WORKFLOW_TEMPLATES.filter((t) => t.status === "active");
    const coming = WORKFLOW_TEMPLATES.filter((t) => t.status === "coming");
    expect(active).toHaveLength(13);
    expect(coming).toHaveLength(0);
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

  it("pins the validate-evidence-on-review template's gate shape", () => {
    // JEH-1114 phase-2 ext PR 2: pick this template, save (no further
    // edits required), and a transition to in_review auto-flips to done
    // only when the issue thread carries a PR URL or image.
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "validate-evidence-on-review");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("status_changed");
    expect(t!.defaults!.action_type).toBe("set_status");
    const conds = t!.defaults!.conditions as Array<{
      op: string;
      value: { require: string[]; lookback_comments: number };
    }>;
    expect(conds).toHaveLength(1);
    expect(conds[0]!.op).toBe("evidence_present");
    expect(conds[0]!.value.require).toEqual(["pr_url", "image_attachment"]);
    expect(conds[0]!.value.lookback_comments).toBe(20);
  });

  it("pins the escalate-stalled-to-owner template's threshold shape", () => {
    // JEH-1114 phase-2 ext PR 3: pick this template, save (no required
    // fields), and the engine walks the parent_issue_id chain on
    // due_date_reached when the issue hasn't moved in 12h.
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "escalate-stalled-to-owner");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("due_date_reached");
    expect(t!.defaults!.action_type).toBe("escalate_to_owner");
    const cfg = t!.defaults!.action_config as { max_age_hours: number };
    expect(cfg.max_age_hours).toBe(12);
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

  // --- Phase 3 (JEH-1108) ---

  it("pins the all-children-done template's set_status shape", () => {
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "all-children-done");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("all_children_done");
    expect(t!.defaults!.action_type).toBe("set_status");
    const cfg = t!.defaults!.action_config as { status: string };
    expect(cfg.status).toBe("in_review");
  });

  it("pins the mention-agent template's comment_mention → auto-reply shape", () => {
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "mention-agent");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("comment_mention");
    // Action: post a comment back on the same issue that mentions the
    // configured agent so it gets queued. The mention link uses the phase-3
    // {{agent_id}} shorthand (substituted from trigger_config.target by
    // actionCommentOnIssue).
    expect(t!.defaults!.action_type).toBe("comment_on_issue");
    const tc = t!.defaults!.trigger_config as { match_mode: string; target: string };
    expect(tc.match_mode).toBe("agent");
    expect(tc.target).toBe("");
    const cfg = t!.defaults!.action_config as { target: string; content: string };
    expect(cfg.target).toBe("self");
    expect(cfg.content).toContain("mention://agent");
    expect(cfg.content).toContain("{{agent_id}}");
  });

  it("pins the cron template's 09:00 Europe/Copenhagen schedule", () => {
    // RFC notes either "0 9 * * *" (daily) or "0 9 * * 1-5" (hverdage) is
    // acceptable for this template — assertion accepts both so a future
    // tweak doesn't break the pin.
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "cron");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("cron");
    const tc = t!.defaults!.trigger_config as {
      schedule_expr: string;
      timezone: string;
    };
    expect(["0 9 * * *", "0 9 * * 1-5"]).toContain(tc.schedule_expr);
    expect(tc.timezone).toBe("Europe/Copenhagen");
  });

  it("pins the sub-issue-created template's comment_on_issue → parent shape", () => {
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "sub-issue-created");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("sub_issue_created");
    expect(t!.defaults!.action_type).toBe("comment_on_issue");
    const cfg = t!.defaults!.action_config as { target: string; content: string };
    expect(cfg.target).toBe("parent");
    expect(cfg.content).toContain("{{title}}");
  });

  it("pins the cron-daily-standup template's hverdag 09:00 schedule", () => {
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "cron-daily-standup");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("cron");
    const tc = t!.defaults!.trigger_config as {
      schedule_expr: string;
      timezone: string;
    };
    // Mandag–fredag kl. 09:00.
    expect(tc.schedule_expr).toBe("0 9 * * 1-5");
    expect(tc.timezone).toBe("Europe/Copenhagen");
    expect(t!.defaults!.action_type).toBe("run_skill");
    const cfg = t!.defaults!.action_config as { skill_name: string; agent_id: string };
    // skill_name pre-udfyldt — agent_id stadig blank (brugeren vælger ejer-agent).
    expect(cfg.skill_name).toBe("daily-standup-summary");
    expect(cfg.agent_id).toBe("");
  });

  it("pins the webhook-github-pr template's webhook_inbound → create_sub_issue shape", () => {
    const t = WORKFLOW_TEMPLATES.find((x) => x.key === "webhook-github-pr");
    expect(t).toBeDefined();
    expect(t!.defaults!.trigger_type).toBe("webhook_inbound");
    expect(t!.defaults!.action_type).toBe("create_sub_issue");
    const cfg = t!.defaults!.action_config as { title: string; description: string };
    // Title can be either a literal (RFC option a, no engine extension) or
    // a {{payload.x}} placeholder if engine is extended. Both forms count
    // as "valid" — the assertion just pins that *some* title and a
    // description are present.
    expect(cfg.title.length).toBeGreaterThan(0);
    expect(cfg.description.length).toBeGreaterThan(0);
  });
});
