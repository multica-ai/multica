// Issue prefixes (workspace-level and project-level) share one grammar:
// 1–10 characters from A–Z and 0–9. The server trims and upper-cases whatever
// it receives, so the client mirrors that normalization — the value the user
// sees in the input is the value that gets persisted, and the preview can
// never promise an identifier the server would reject.
export const ISSUE_PREFIX_MAX_LENGTH = 10;

const ISSUE_PREFIX_PATTERN = /^[A-Z0-9]{1,10}$/;

export function normalizeIssuePrefix(raw: string): string {
  return raw
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, "")
    .slice(0, ISSUE_PREFIX_MAX_LENGTH);
}

export function isValidIssuePrefix(value: string): boolean {
  return ISSUE_PREFIX_PATTERN.test(value);
}
