import type { QuestionnaireAnswers, Role, UseCase } from "./types";

/**
 * Identifier for the four legacy onboarding agent templates. Keep in
 * sync with the template registry inside StepAgent in
 * `packages/views/onboarding/steps/step-agent.tsx`.
 */
export type AgentTemplateId = "coding" | "planning" | "writing" | "assistant";

/**
 * Pick a recommended agent template based on the questionnaire
 * (role × use_case). Role is the primary signal; use_case is a
 * tiebreaker for roles that legitimately split between templates
 * (engineer / product / marketing).
 *
 * `use_case` is multi-select — when a user picks several, the rules
 * below use `.includes(...)` against the set. Order of evaluation
 * inside each role's switch is the implicit priority (first match
 * wins). For ambiguous overlaps (e.g. engineer who picks both
 * `manage_team` and `write_publish`) the earlier branch wins, which
 * matches the prior single-select behavior when only one of those
 * was selectable.
 *
 * Fallback chain when role is skipped or null:
 *   1. Derive from use_case alone (same priority order).
 *   2. Both unknown → `assistant` (the generic default).
 *
 * Pure / deterministic — safe to call on every render.
 */
export function recommendTemplate(
  answers: Pick<QuestionnaireAnswers, "role" | "use_case">,
): AgentTemplateId {
  const role: Role | null = answers.role;
  const useCases: readonly UseCase[] = answers.use_case ?? [];

  if (role === null) return fallbackFromUseCase(useCases);

  switch (role) {
    case "engineer":
      if (useCases.includes("manage_team") || useCases.includes("plan_research"))
        return "planning";
      if (useCases.includes("write_publish")) return "writing";
      return "coding";
    case "product":
      if (useCases.includes("ship_code")) return "coding";
      return "planning";
    case "designer":
      return "assistant";
    case "writer":
      return "writing";
    case "marketing":
      if (useCases.includes("write_publish") || useCases.includes("plan_research"))
        return "writing";
      return "planning";
    case "research":
      return "planning";
    case "founder":
    case "ops":
    case "student":
    case "other":
      return "assistant";
  }
}

function fallbackFromUseCase(useCases: readonly UseCase[]): AgentTemplateId {
  if (useCases.includes("ship_code")) return "coding";
  if (useCases.includes("write_publish")) return "writing";
  if (useCases.includes("manage_team") || useCases.includes("plan_research"))
    return "planning";
  return "assistant";
}
