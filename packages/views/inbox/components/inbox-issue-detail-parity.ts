// CEREBRO-PATCH(inbox-issue-detail-parity): FIR-4918 — shared defaults so Inbox
// IssueDetail matches the issue page (sidebar open with References/Properties).
// layoutId is versioned because useDefaultLayout persists layout to localStorage
// on mount; without a bump, existing readers keep "sidebar collapsed" forever.
export const INBOX_ISSUE_DETAIL_DEFAULT_SIDEBAR_OPEN = true as const;
export const INBOX_ISSUE_DETAIL_LAYOUT_ID =
  "multica_inbox_issue_detail_layout_v2" as const;
