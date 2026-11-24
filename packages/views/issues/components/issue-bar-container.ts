/**
 * The bars that show an issue's status, assignee and quick actions as chips:
 * the Inbox's floating group above the composer, and the issue page header
 * while the properties sidebar is closed. Each is a named size container, and
 * its chips drop their labels as it narrows.
 *
 * The class names are spelled out in full so Tailwind can find them.
 */
export type IssueBarContainer = "inbox-bar" | "issue-header";

export const ISSUE_BAR_HIDE_LABEL: Record<IssueBarContainer, string> = {
  "inbox-bar": "@max-[40rem]/inbox-bar:hidden",
  // The header also holds the breadcrumb and the issue title, so it needs more
  // room than the Inbox bar before the chips can afford their labels.
  "issue-header": "@max-[60rem]/issue-header:hidden",
};
