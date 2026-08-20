// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { InboxItem } from "@multica/core/types";
import {
  getInboxDisplayTitle,
  getQuickCreateOutcomeDetail,
  isQuickCreateOutcome,
  resolveDetailItem,
  stripQuickCreatePrefix,
} from "./inbox-display";

function item(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-04-29T12:00:00Z",
    details: null,
    ...overrides,
  };
}

describe("inbox display helpers", () => {
  it("removes legacy quick-create created prefixes from list titles", () => {
    expect(
      stripQuickCreatePrefix(
        "Created MUL-1583: Fix agent list column widths",
        "MUL-1583",
      ),
    ).toBe("Fix agent list column widths");
  });

  it("cleans quick-create success titles before rendering the inbox row", () => {
    const quickCreateItem = item({
      type: "quick_create_done",
      title: "Created MUL-1583: Fix agent list column widths",
      details: { identifier: "MUL-1583" },
    });

    expect(getInboxDisplayTitle(quickCreateItem)).toBe(
      "Fix agent list column widths",
    );
  });

  it("uses the original prompt as the failed quick-create row title", () => {
    const failedItem = item({
      type: "quick_create_failed",
      title: "Quick create failed",
      body: "agent finished without creating an issue",
      issue_id: null,
      details: {
        original_prompt: "Optimize QuickCapture UI\nand attached screenshot",
      },
    });

    expect(getInboxDisplayTitle(failedItem)).toBe(
      "Optimize QuickCapture UI and attached screenshot",
    );
  });

  it("uses the redacted failure detail for failed quick-create subtitles", () => {
    const failedItem = item({
      type: "quick_create_failed",
      body: "fallback body",
      details: { error: "CLI failed\nwith exit status 1" },
    });

    expect(getQuickCreateOutcomeDetail(failedItem)).toBe(
      "CLI failed with exit status 1",
    );
  });

  it("treats both non-success quick-create outcomes as recoverable rows", () => {
    // Drives the original-prompt box and the "Edit as advanced form" recovery
    // affordance in inbox-page.tsx — the unconfirmed row must keep both.
    expect(isQuickCreateOutcome("quick_create_failed")).toBe(true);
    expect(isQuickCreateOutcome("quick_create_unconfirmed")).toBe(true);
    expect(isQuickCreateOutcome("quick_create_done")).toBe(false);
    expect(isQuickCreateOutcome("new_comment")).toBe(false);
  });

  it("uses the original prompt as the unconfirmed quick-create row title", () => {
    const unconfirmedItem = item({
      type: "quick_create_unconfirmed",
      title: "Quick create needs a check",
      body: "Couldn't confirm whether the issue was created.",
      issue_id: null,
      details: { original_prompt: "File a bug about\nthe flaky test" },
    });

    expect(getInboxDisplayTitle(unconfirmedItem)).toBe(
      "File a bug about the flaky test",
    );
  });
});

describe("resolveDetailItem", () => {
  // Rows the inbox keys by issue_id, plus one plain notification that has none
  // and is therefore keyed by its own id.
  const a = item({ id: "inbox-a", issue_id: "issue-a" });
  const b = item({ id: "inbox-b", issue_id: "issue-b" });
  const plain = item({ id: "inbox-plain", issue_id: null });
  const list = [a, b, plain];

  it("holds the row the pane is still on while the list has moved on", () => {
    expect(resolveDetailItem(list, "issue-b", "issue-a")).toBe(a);
  });

  it("shows the live selection once the pane has caught up", () => {
    expect(resolveDetailItem(list, "issue-b", "issue-b")).toBe(b);
  });

  it("keys a notification without an issue by its own id", () => {
    expect(resolveDetailItem(list, "inbox-plain", "inbox-plain")).toBe(plain);
  });

  // Archive moves the selection to the neighbour and drops the actioned row in
  // the same commit, so the deferred key resolves to nothing. Holding that miss
  // would blink the empty state between the two issues.
  it("falls forward to the live selection when the held row is gone", () => {
    expect(resolveDetailItem([b, plain], "issue-b", "issue-a")).toBe(b);
  });

  it("stays empty while the first selection is still rendering", () => {
    expect(resolveDetailItem(list, "issue-a", "")).toBeNull();
  });

  it("stays empty when the selection is cleared and the pane agrees", () => {
    expect(resolveDetailItem(list, "", "")).toBeNull();
  });

  // Deep link to a notification that is not in this user's inbox: the page
  // redirects to the issue page, and the pane must not invent a row meanwhile.
  it("resolves nothing when neither key is in the list", () => {
    expect(resolveDetailItem(list, "issue-gone", "issue-also-gone")).toBeNull();
  });
});
