/**
 * Builds a markdown blockquote that documents who the user is on each
 * starter issue Helper picks up. Source data is the onboarding
 * questionnaire stashed on `user.onboarding_questionnaire`.
 *
 * The block is APPENDED (not prepended) so the original prompt stays
 * the lead — Helper reads the task instruction first, then the user
 * context as supplementary info. Returns "" when there's nothing
 * useful to say (questionnaire empty, everything skipped) so the
 * caller can unconditionally do:
 *   description: prompt + buildUserContextSection(...)
 *
 * Labels (heading, "Role", "Use case", slug→label maps) are passed
 * in by the caller after resolving from i18n. Keeping this function
 * pure makes it easy to unit-test and easy to call from the workspace
 * shell without dragging an i18n hook into a templates module.
 */

export interface UserContextLabels {
  /** Section heading shown above the list (e.g. "About me" / "关于我"). */
  heading: string;
  /** Label for the role line (e.g. "Role" / "角色"). */
  roleLabel: string;
  /** Label for the use case line (e.g. "What I want to do" / "想用来做"). */
  useCaseLabel: string;
  /** Joiner for multi-select use_case values (", " for EN, "、" for ZH). */
  listSeparator: string;
  /** slug → human-readable label for each role enum value. */
  role: Record<string, string>;
  /** slug → human-readable label for each use_case enum value. */
  useCase: Record<string, string>;
}

export interface QuestionnaireRaw {
  role?: unknown;
  role_other?: unknown;
  use_case?: unknown;
  use_case_other?: unknown;
}

function asStringArray(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter(
      (v): v is string => typeof v === "string" && v.length > 0,
    );
  }
  // Tolerate the legacy single-string shape so questionnaires written
  // before the multi-select migration still render correctly.
  if (typeof value === "string" && value.length > 0) return [value];
  return [];
}

function asString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

export function buildUserContextSection(
  raw: QuestionnaireRaw | undefined | null,
  labels: UserContextLabels,
): string {
  if (!raw) return "";

  // Role is single-select. "other" slug means the user picked Other and
  // filled the free-text input — show the free-text instead of the
  // generic "Other" label.
  const role = asString(raw.role);
  const roleOther = asString(raw.role_other);
  const roleDisplay =
    role === "other"
      ? roleOther
      : role
        ? labels.role[role] ?? role
        : "";

  // Use case is multi-select. The "other" slug stacks alongside regular
  // picks; the free-text lives in use_case_other. Map each slug to a
  // human label, drop empties, and join with the locale-appropriate
  // separator.
  const useCaseSlugs = asStringArray(raw.use_case);
  const useCaseOther = asString(raw.use_case_other);
  const useCaseDisplays = useCaseSlugs
    .map((slug) =>
      slug === "other" ? useCaseOther : labels.useCase[slug] ?? slug,
    )
    .filter((s) => s.length > 0);

  const hasRole = roleDisplay.length > 0;
  const hasUseCase = useCaseDisplays.length > 0;
  if (!hasRole && !hasUseCase) return "";

  // Two leading newlines + horizontal rule separate the block from the
  // prompt above it so renderers don't fuse them into the same paragraph.
  const lines: string[] = ["", "", "---", "", `> **${labels.heading}**`, ">"];
  if (hasRole) lines.push(`> ${labels.roleLabel}: ${roleDisplay}`);
  if (hasUseCase) {
    lines.push(
      `> ${labels.useCaseLabel}: ${useCaseDisplays.join(labels.listSeparator)}`,
    );
  }
  return lines.join("\n");
}
