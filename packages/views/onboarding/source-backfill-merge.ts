import type {
  QuestionnaireAnswers,
  Role,
  UseCase,
} from "@multica/core/onboarding";

/**
 * `PATCH /api/me/onboarding` overwrites the JSONB column wholesale —
 * it does NOT JSONB-merge (see `PatchUserOnboarding` in
 * `server/internal/handler/onboarding.go`). For the backfill flow the
 * user's stored questionnaire already contains role / use_case /
 * version, so we must read those, overlay only the source-related
 * fields, and send the whole shape back. Otherwise the very users we
 * target — onboarded accounts with role/use_case answered but source
 * missing — would have their other answers wiped on Submit / Skip.
 *
 * Mirrors the legacy tolerance in `OnboardingFlow.mergeQuestionnaire`
 * and the server's `stringOrSlice` decoder: `source` and `use_case`
 * may exist on disk as a bare string from a prior single-select
 * schema, which we coerce to a single-element array.
 */
export function mergedQuestionnairePatch(
  stored: Record<string, unknown> | null | undefined,
  sourcePatch: Pick<
    QuestionnaireAnswers,
    "source" | "source_other" | "source_skipped"
  >,
): QuestionnaireAnswers {
  const raw = (stored ?? {}) as Record<string, unknown>;
  return {
    source: sourcePatch.source,
    source_other: sourcePatch.source_other,
    source_skipped: sourcePatch.source_skipped,
    role: coerceRole(raw.role),
    role_other: coerceString(raw.role_other),
    role_skipped: coerceBool(raw.role_skipped),
    use_case: coerceStringArray<UseCase>(raw.use_case),
    use_case_other: coerceString(raw.use_case_other),
    use_case_skipped: coerceBool(raw.use_case_skipped),
    version: 2,
  };
}

function coerceStringArray<T extends string>(value: unknown): T[] {
  if (Array.isArray(value)) {
    return value.filter((v): v is T => typeof v === "string" && v.length > 0);
  }
  if (typeof value === "string" && value.length > 0) {
    return [value as T];
  }
  return [];
}

function coerceString(value: unknown): string | null {
  if (typeof value !== "string") return null;
  return value.length > 0 ? value : null;
}

function coerceBool(value: unknown): boolean {
  return value === true;
}

function coerceRole(value: unknown): Role | null {
  return coerceString(value) as Role | null;
}
