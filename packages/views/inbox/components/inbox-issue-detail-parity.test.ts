import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  INBOX_ISSUE_DETAIL_DEFAULT_SIDEBAR_OPEN,
  INBOX_ISSUE_DETAIL_LAYOUT_ID,
} from "./inbox-issue-detail-parity";

const inboxPage = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), "inbox-page.tsx"),
  "utf8",
);

describe("inbox issue-detail parity (FIR-4918)", () => {
  it("opens the sidebar by default so Properties/Subscribers match the issue page", () => {
    expect(INBOX_ISSUE_DETAIL_DEFAULT_SIDEBAR_OPEN).toBe(true);
  });

  // Deliberate product decision (2026-08-10): a reader's saved pane width is
  // theirs, so the layoutId is NOT versioned. The cost is that an existing
  // reader keeps the collapsed sidebar until they drag it open once. This
  // asserts the unversioned id so the bump cannot come back by accident.
  it("keeps the original layoutId so no saved pane layout is discarded", () => {
    expect(INBOX_ISSUE_DETAIL_LAYOUT_ID).toBe(
      "multica_inbox_issue_detail_layout",
    );
    expect(INBOX_ISSUE_DETAIL_LAYOUT_ID).not.toMatch(/_v\d+$/);
  });

  // Asserting the constants alone proves nothing: inbox-page.tsx could stop
  // using them (or go back to a literal `false`) and both assertions above
  // would still pass. These lock the call site to the constants — and the
  // import, which the first cut of this module left out entirely.
  it("wires those defaults into the IssueDetail the inbox renders", () => {
    expect(inboxPage).toContain(
      'from "./inbox-issue-detail-parity"',
    );
    expect(inboxPage).toContain(
      "defaultSidebarOpen={INBOX_ISSUE_DETAIL_DEFAULT_SIDEBAR_OPEN}",
    );
    expect(inboxPage).toContain("layoutId={INBOX_ISSUE_DETAIL_LAYOUT_ID}");
  });

  it("never collapses the sidebar at the call site", () => {
    expect(inboxPage).toContain("<IssueDetail");
    expect(inboxPage).not.toContain("defaultSidebarOpen={false}");
  });
});
