import { z } from "zod";
import type {
  CompanyTemplateApplyResult,
  CompanyTemplateStep,
  CompanyTemplateStepStatus,
} from "./company-template-types";

// The apply response is read with parseWithFallback. Steps decide whether the
// wizard reports success, so a malformed step fails the whole body and the
// wizard shows "not confirmed" rather than a partial success. The other fields
// degrade on their own: they only inform the summary.

const KNOWN_STEP_STATUSES: readonly CompanyTemplateStepStatus[] = ["created", "reused", "failed"];

/** An unknown status is a failure: the wizard never reports a step it cannot read as done. */
const StepStatusSchema = z
  .string()
  .catch("failed")
  .transform((value): CompanyTemplateStepStatus =>
    (KNOWN_STEP_STATUSES as readonly string[]).includes(value)
      ? (value as CompanyTemplateStepStatus)
      : "failed",
  );

export const CompanyTemplateStepSchema = z
  .object({
    key: z.string().default(""),
    status: StepStatusSchema,
    message: z.string().default(""),
  })
  .loose();

export const CompanyTemplateApplyResultSchema = z
  .object({
    steps: z.array(CompanyTemplateStepSchema).default([]),
    pipeline_id: z.string().nullable().catch(null).default(null),
    unowned_stages: z.array(z.string()).catch([]).default([]),
  })
  .loose();

/**
 * What an unreadable response degrades to. It has no steps, so the wizard
 * reports that the result could not be confirmed and offers a rerun, which is
 * safe because the apply is idempotent.
 */
export const FALLBACK_COMPANY_TEMPLATE_RESULT: CompanyTemplateApplyResult = {
  steps: [],
  pipeline_id: null,
  unowned_stages: [],
};

/** Whether a result confirms the whole company: at least one step, and none failed. */
export function isCompanyTemplateComplete(result: CompanyTemplateApplyResult): boolean {
  return result.steps.length > 0 && result.steps.every((step: CompanyTemplateStep) => step.status !== "failed");
}
