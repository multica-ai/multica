// CEREBRO-PATCH(inbox-issue-detail-parity): FIR-4918 — shared defaults so the
// Inbox IssueDetail matches the issue page (sidebar open, References/Properties).
//
// layoutId is deliberately NOT versioned: a reader's saved pane layout is theirs
// and must not be discarded. Accepted consequence — useDefaultLayout persists the
// layout to localStorage on mount and restores it over the per-panel defaultSize,
// so a reader who has already opened a message keeps the collapsed sidebar until
// they drag it open once. A new browser gets the new default immediately.
export const INBOX_ISSUE_DETAIL_DEFAULT_SIDEBAR_OPEN = true as const;
export const INBOX_ISSUE_DETAIL_LAYOUT_ID =
  "multica_inbox_issue_detail_layout" as const;
