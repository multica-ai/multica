import type { QuestionnaireAnswers } from "./types";

/**
 * Identifier for the four agent templates offered during onboarding Step 4.
 * Keep in sync with the template registry inside StepAgent in
 * `packages/views/onboarding/steps/step-agent.tsx`.
 */
export type AgentTemplateId = "coding" | "planning" | "writing" | "assistant";

/**
 * Pick a recommended agent template for a user based on their
 * questionnaire answers. Role is treated as the primary signal (who the
 * user is); use_case is only a tiebreaker for roles that legitimately
 * split between templates (developer / product_lead).
 *
 * `role = other` and `role = founder` both fall back to the generic
 * Assistant: "other" means the user declined to claim a role, and
 * "founder" means they wear every hat, so a single specialized agent is
 * a poor default.
 *
 * Pure / deterministic — safe to call on every render.
 */
export function recommendTemplate(
  answers: Pick<QuestionnaireAnswers, "role" | "use_case">,
): AgentTemplateId {
  const { role, use_case } = answers;

  if (role === "other" || role === "founder") return "assistant";
  if (role === "writer") return "writing";

  if (role === "developer") {
    return use_case === "planning" ? "planning" : "coding";
  }

  if (role === "product_lead") {
    return use_case === "coding" ? "coding" : "planning";
  }

  // Unknown / null role — user hasn't answered Q2 yet.
  return "assistant";
}
