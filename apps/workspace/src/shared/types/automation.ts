// Type definitions for the automation template system.

/** Built-in automation template as returned by the API. */
export interface AutomationTemplate {
  id: string;
  name: string;
  description: string;
  /** "scheduled" | "manual" */
  trigger_type: string;
  /** Cron-like schedule description, only present for scheduled templates. */
  schedule?: string;
  icon: string;
  /** Whether this template is currently enabled for the workspace. */
  enabled: boolean;
}

/** Response from POST /api/automation/rules/:template_id/run */
export interface StandupSummaryResult {
  template_id: string;
  content: string;
  date: string;
  member_count: number;
}
