const TITLE_MAX_LENGTH = 60;

/** Keep browser-tab titles scannable when several Multica pages are open. */
export function truncatePageTitle(value: string, maxLength = TITLE_MAX_LENGTH) {
  const normalized = value.replace(/\s+/g, " ").trim();
  if (normalized.length <= maxLength) return normalized;
  return `${normalized.slice(0, Math.max(0, maxLength - 1)).trimEnd()}…`;
}

export function formatIssuePageTitle(identifier?: string | null, title?: string | null) {
  const issueIdentifier = identifier?.trim();
  const issueTitle = title?.trim();

  if (issueIdentifier && issueTitle) {
    return `${issueIdentifier} ${truncatePageTitle(issueTitle)}`;
  }
  return issueIdentifier || (issueTitle ? truncatePageTitle(issueTitle) : "Issue");
}

export function formatEntityPageTitle(pageName: string, entityName?: string | null) {
  const name = entityName?.trim();
  return name ? `${pageName} · ${truncatePageTitle(name)}` : pageName;
}
