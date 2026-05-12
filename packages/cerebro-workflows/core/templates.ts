import type { WorkflowWriteInput } from "./types";

/**
 * Default-trigger startpakke fra [JEH-1047](https://multica.io). Seks templates
 * der dækker de mest brugte cases. To er aktive i fase 1 (status-change og
 * due-date/time); de øvrige er placeholders for fase-2 triggers, vist i
 * pickeren med en "kommer snart"-disabled state så listen matcher spec'en
 * og brugerne ved hvad der er på vej.
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
    key: "all-children-done",
    label: "All children done → Update parent",
    description:
      "Når alle sub-issues er done, flyt parent til in_review. Kommer i fase 2 sammen med all_children_done-triggeren.",
    status: "coming",
  },
  {
    key: "mention-agent",
    label: "Mention agent in comment → Run agent",
    description:
      "@mention en agent i en kommentar for at sætte den i gang. Kommer i fase 2 med comment-trigger.",
    status: "coming",
  },
  {
    key: "cron",
    label: "Run on schedule",
    description:
      "Kør et workflow på cron-skema (fx hver morgen). Kommer i fase 2 med cron-trigger.",
    status: "coming",
  },
  {
    key: "sub-issue-created",
    label: "Sub-issue created → Notify parent owner",
    description:
      "Notificér parent-issuets ejer når der oprettes et nyt sub-issue. Kommer i fase 2 med sub-issue-trigger.",
    status: "coming",
  },
];
