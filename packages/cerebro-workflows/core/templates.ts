import type { WorkflowWriteInput } from "./types";

/**
 * Default-trigger startpakke fra [JEH-1047](https://multica.io). Phase 1 shipped
 * the first two active templates (status-change, due-date-time); phase 2
 * ([JEH-1103]) added run-skill and comment-on-issue. Phase 3 ([JEH-1108])
 * activates the four "coming" placeholders (all-children-done,
 * mention-agent, cron, sub-issue-created) and adds two new entries
 * (cron-daily-standup, webhook-github-pr) — for a total of 10 active
 * templates with no remaining "coming" placeholders.
 *
 * Templates er kun *defaults* — formularen pre-udfylder felter ud fra
 * `defaults`, og brugeren skal trykke Gem for at oprette en aktiv regel.
 * Ingen workflow_row oprettes automatisk ved workspace-oprettelse.
 */
export interface WorkflowTemplate {
  /** Stable kebab-case key (used for the "use template" button + tests). */
  key: string;
  /** Display label in the template picker. */
  label: string;
  /** Short helper text shown under the label. */
  description: string;
  /** "active" templates pre-fill the form. "coming" are visible-but-disabled. */
  status: "active" | "coming";
  /** Pre-filled form values. Only set for active templates. */
  defaults?: WorkflowWriteInput;
}

export const WORKFLOW_TEMPLATES: ReadonlyArray<WorkflowTemplate> = [
  {
    key: "status-change",
    label: "Status changed → Create sub-issue",
    description: "Når et issue går i in_review, opret et QA sub-issue.",
    status: "active",
    defaults: {
      name: "Auto QA on in_review",
      enabled: true,
      trigger_type: "status_changed",
      trigger_config: { to_status: "in_review" },
      conditions: [],
      action_type: "create_sub_issue",
      action_config: {
        title: "QA: {{title}}",
        description: "Auto-genereret fra workflow ved overgang til in_review.",
      },
    },
  },
  {
    key: "due-date-time",
    label: "Due date / time reached → Send reminder",
    description:
      "Ping den tildelte agent eller member når et issues due date (eller specifikke klokkeslæt) er nået.",
    status: "active",
    defaults: {
      name: "Due-date reminder",
      enabled: true,
      trigger_type: "due_date_reached",
      trigger_config: {},
      conditions: [],
      action_type: "send_reminder",
      action_config: {
        // Recipient is intentionally blank — the user fills in the right
        // member/agent UUID before saving. UI surfaces the validation
        // error if they try to save without it.
        recipient_id: "",
        recipient_type: "member",
        message: "Reminder: issuet's due date er nået.",
      },
    },
  },
  {
    key: "run-skill",
    label: "Status changed → Run skill",
    description:
      "Når et issue går i in_review, kør en navngiven skill på den valgte agent med issue-konteksten som input.",
    status: "active",
    defaults: {
      name: "Auto skill on in_review",
      enabled: true,
      trigger_type: "status_changed",
      trigger_config: { to_status: "in_review" },
      conditions: [],
      action_type: "run_skill",
      action_config: {
        // skill_name and agent_id are intentionally blank — the user picks
        // the skill name (must already exist in the workspace) and the agent
        // that owns the skill bundle before saving.
        skill_name: "",
        agent_id: "",
        skill_input: {
          issue_title: "{{title}}",
          issue_status: "{{status}}",
        },
      },
    },
  },
  {
    key: "comment-on-issue",
    label: "Status changed → Comment on issue",
    description:
      "Når et issue ændrer status, post en workflow-forfattet kommentar på issuet eller dets parent.",
    status: "active",
    defaults: {
      name: "Status announcement",
      enabled: true,
      trigger_type: "status_changed",
      trigger_config: { to_status: "in_review" },
      conditions: [],
      action_type: "comment_on_issue",
      action_config: {
        target: "self",
        content: "Workflow: {{title}} er nu klar til review.",
      },
    },
  },
  {
    // Phase-2 ext (JEH-1114, PR 1). Composes med phase-1 conditions:
    // workflows der filtrerer på `issue.labels` kan nu reagere automatisk
    // når et issue er klassificeret. Workspacet skal have et label per
    // domæne (fx `domain:code`) før templaten er funktionsdygtig.
    key: "route-by-domain",
    label: "Status changed → Route by domain",
    description:
      "Når et issue går i todo, klassificér det til kode/business/design/indhold og attach et `domain:<x>` label.",
    status: "active",
    defaults: {
      name: "Auto-route by domain",
      enabled: true,
      trigger_type: "status_changed",
      trigger_config: { to_status: "todo" },
      conditions: [],
      action_type: "route_by_domain",
      action_config: {
        label_prefix: "domain:",
        default_domain: "business",
      },
    },
  },
  {
    // Phase-2 ext (JEH-1114, PR 2). The condition op `evidence_present`
    // gates `set_status: done` on a PR URL / image / regex match across
    // the issue's recent comments + description + attachments. Form
    // builder doesn't yet expose a condition editor (pre-existing gap),
    // so this template is the canonical way to set up the workflow until
    // a future PR adds the UI.
    key: "validate-evidence-on-review",
    label: "In review + evidence → Done",
    description:
      "Når et issue går i in_review og tråden indeholder en PR-URL eller screenshot, flyt automatisk til done. Ellers bliver det i in_review.",
    status: "active",
    defaults: {
      name: "Auto-done når evidens er på plads",
      enabled: true,
      trigger_type: "status_changed",
      trigger_config: { to_status: "in_review" },
      conditions: [
        {
          op: "evidence_present",
          value: {
            require: ["pr_url", "image_attachment"],
            lookback_comments: 20,
          },
        },
      ],
      action_type: "set_status",
      action_config: {
        status: "done",
      },
    },
  },
  {
    key: "all-children-done",
    label: "All children done → Update parent",
    description: "Når alle children er done, flyt parent til in_review.",
    status: "active",
    defaults: {
      name: "Auto promote parent when all children done",
      enabled: true,
      trigger_type: "all_children_done",
      // No trigger_config fields — the listener already scopes by the
      // triggered issue's parent so workspace-wide misfires aren't possible.
      trigger_config: {},
      conditions: [],
      action_type: "set_status",
      action_config: {
        status: "in_review",
      },
    },
  },
  {
    key: "mention-agent",
    label: "Mention agent in comment → Auto-reply",
    description: "Når en agent @mentiones i en kommentar, re-route issue til den.",
    status: "active",
    defaults: {
      name: "Mention → auto-reply",
      enabled: true,
      trigger_type: "comment_mention",
      trigger_config: {
        // Mode "agent" matches a comment that @mentions the agent referenced
        // by target. Target stays blank until the user picks the agent UUID.
        match_mode: "agent",
        target: "",
      },
      conditions: [],
      action_type: "comment_on_issue",
      action_config: {
        target: "self",
        // Posts a mention link back to the configured agent so it gets
        // queued. {{agent_id}} is substituted from trigger_config.target by
        // the comment-on-issue action helper (phase-3 extension).
        content: "[@<agent>](mention://agent/{{agent_id}})",
      },
    },
  },
  {
    key: "cron",
    label: "Run on schedule",
    description: "Kør en navngivet skill på et fast skema.",
    status: "active",
    defaults: {
      name: "Run skill on schedule",
      enabled: true,
      trigger_type: "cron",
      trigger_config: {
        // Hver dag kl. 09:00 Europe/Copenhagen — server-side sweeper bruger
        // robfig/cron/v3 til at parse udtrykket. cron-daily-standup nedenfor
        // har en mere snæver "kun hverdage" variant.
        schedule_expr: "0 9 * * *",
        timezone: "Europe/Copenhagen",
      },
      conditions: [],
      action_type: "run_skill",
      action_config: {
        // Blank — user picks skill_name + agent_id before saving.
        skill_name: "",
        agent_id: "",
        skill_input: {},
      },
    },
  },
  {
    key: "sub-issue-created",
    label: "Sub-issue created → Comment on parent",
    description: "Notificér parent når der oprettes nye sub-issues.",
    status: "active",
    defaults: {
      name: "Nyt sub-issue → kommentar på parent",
      enabled: true,
      trigger_type: "sub_issue_created",
      trigger_config: {
        // Empty parent_issue_id matches any sub-issue in the workspace.
        // User can pin to a specific parent UUID before saving.
        parent_issue_id: "",
      },
      conditions: [],
      action_type: "comment_on_issue",
      action_config: {
        target: "parent",
        content: "Nyt sub-issue oprettet: {{title}}",
      },
    },
  },
  {
    key: "cron-daily-standup",
    label: "Daily stand-up → Run skill",
    description: "Daglig standup-summary på hverdage kl. 9.",
    status: "active",
    defaults: {
      name: "Daily stand-up skill",
      enabled: true,
      trigger_type: "cron",
      trigger_config: {
        // Mandag–fredag kl. 09:00 Europe/Copenhagen.
        schedule_expr: "0 9 * * 1-5",
        timezone: "Europe/Copenhagen",
      },
      conditions: [],
      action_type: "run_skill",
      action_config: {
        // skill_name pre-fyldt med spec'ens navne-suggestion; agent_id blank
        // så brugeren vælger den agent der ejer skillen før gem.
        skill_name: "daily-standup-summary",
        agent_id: "",
        skill_input: {},
      },
    },
  },
  {
    key: "webhook-github-pr",
    label: "GitHub PR webhook → Create sub-issue",
    description: "Inbound webhook fra GitHub PR-event → opret sub-issue.",
    status: "active",
    defaults: {
      name: "GitHub PR webhook → sub-issue",
      enabled: true,
      trigger_type: "webhook_inbound",
      // No trigger_config for inbound webhooks — the token + secret live on
      // the workflow row (regenerate-token endpoint mints them).
      trigger_config: {},
      conditions: [],
      action_type: "create_sub_issue",
      action_config: {
        // Phase-3 renderTemplate extension allows arbitrary {{payload.x.y}}
        // dotted-path lookups against te.Raw["payload"] (the parsed webhook
        // body). Missing keys render as empty strings, so a slightly off
        // payload shape degrades gracefully instead of crashing the action.
        title: "GitHub PR: {{payload.pull_request.title}}",
        description: "Ny webhook fra GitHub PR. Se workflow-loggen for payload.",
      },
    },
  },
];
