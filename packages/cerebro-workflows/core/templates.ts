import type { WorkflowWriteInput } from "./types";

/**
 * Default-trigger startpakke fra [JEH-1047](https://multica.io). Phase 1 shipped
 * the first two active templates (status-change, due-date-time); phase 2
 * ([JEH-1103]) adds run-skill and comment-on-issue. The remaining four are
 * still "coming" — they need triggers that don't exist yet (all_children_done,
 * comment_mention, cron, sub_issue_created) and land in a future phase.
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
    key: "all-children-done",
    label: "All children done → Update parent",
    description:
      "Når alle sub-issues er done, flyt parent til in_review. Kommer i fase 3 sammen med all_children_done-triggeren.",
    status: "coming",
  },
  {
    key: "mention-agent",
    label: "Mention agent in comment → Run agent",
    description:
      "@mention en agent i en kommentar for at sætte den i gang. Kommer i fase 3 med comment-trigger.",
    status: "coming",
  },
  {
    key: "cron",
    label: "Run on schedule",
    description:
      "Kør et workflow på cron-skema (fx hver morgen). Kommer i fase 3 med cron-trigger.",
    status: "coming",
  },
  {
    key: "sub-issue-created",
    label: "Sub-issue created → Notify parent owner",
    description:
      "Notificér parent-issuets ejer når der oprettes et nyt sub-issue. Kommer i fase 3 med sub-issue-trigger.",
    status: "coming",
  },
];
