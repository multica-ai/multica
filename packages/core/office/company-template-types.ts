import type { CompanyStart, CompanySquadKey } from "../squads/company-template";

/**
 * The starter company's apply call (`POST /api/office/company-template/apply`).
 * It is idempotent: a rerun reuses what the first run made, and a failed step
 * is reported so the caller can run the call again.
 */

export interface CompanyTemplateApplyRequest {
  /** The runtime the worker agents run on. The Secretary keeps its own. */
  runtime_id: string;
  /** The model for the worker agents. Omitted, the runtime's default applies. */
  model?: string;
  /** The squads to install, by their card keys. Every squad is optional. */
  squads: CompanySquadKey[];
  start: CompanyStart;
}

/**
 * "created" and "reused" are successes. "failed" carries the reason in
 * `message`; a step that failed is safe to retry by running the call again.
 */
export type CompanyTemplateStepStatus = "created" | "reused" | "failed";

export interface CompanyTemplateStep {
  /** A stable name for the step, such as "squad:market_research" or "agent:Market Scout". */
  key: string;
  status: CompanyTemplateStepStatus;
  message: string;
}

export interface CompanyTemplateApplyResult {
  steps: CompanyTemplateStep[];
  /** The installed pipeline, or null when the apply made none. */
  pipeline_id: string | null;
  /** Stage keys the selected squads do not own. */
  unowned_stages: string[];
}
