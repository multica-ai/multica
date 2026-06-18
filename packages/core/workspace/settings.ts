export const AUTO_LABEL_NEW_ISSUES_SETTING = "auto_label_new_issues";

export function isAutoLabelNewIssuesEnabled(
  settings: Record<string, unknown> | null | undefined,
): boolean {
  return settings?.[AUTO_LABEL_NEW_ISSUES_SETTING] === true;
}

export function withAutoLabelNewIssuesSetting(
  settings: Record<string, unknown> | null | undefined,
  enabled: boolean,
): Record<string, unknown> {
  return {
    ...(settings ?? {}),
    [AUTO_LABEL_NEW_ISSUES_SETTING]: enabled,
  };
}
