// CEREBRO-PATCH(inbox-page-stub): fallback placeholder for the upstream inbox.
//
// The cerebro fork has its own inbox implementation in @multica/cerebro-inbox
// (1457 lines, gated by the `cerebro_inbox` feature flag, default-on). When the
// flag is toggled OFF, the route shows this placeholder.
//
// The actual upstream inbox-page.tsx (438 lines) cannot be restored verbatim
// because the fork's surrounding modules (@multica/core/inbox/queries, types,
// IssueDetailProps) have diverged structurally — the upstream source references
// symbols like `useInboxUnreadCount`, `./inbox-display`, and the
// `quick_create_failed` InboxItemType variant that no longer exist in this
// fork's core packages. Restoring those would cascade into restoring large
// parts of inbox + issues + types — out of scope for Phase 2.
//
// Future upstream syncs: this file is the single conflict surface. Resolve by
// keeping THIS placeholder (cerebro semantics) and ignoring upstream's content.

"use client";

export function InboxPage() {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <div className="max-w-md text-center">
        <h2 className="mb-2 text-xl font-semibold">Inbox</h2>
        <p className="text-sm text-muted-foreground">
          This fork uses the cerebro inbox implementation. Enable
          {" "}
          <strong>Cerebro inbox</strong> in Settings → Cerebro features to use it.
        </p>
      </div>
    </div>
  );
}
