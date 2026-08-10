// @vitest-environment jsdom
//
// FIR-4918 — an issue opened from the inbox must show the same fields as the
// same issue opened on its own page. Two separate defects made it show less,
// and both are guarded here:
//
//   1. The sidebar extensions slot mounted Rounds but never References, so
//      References was ABSENT from the inbox, not merely hidden.
//   2. Both inbox surfaces passed defaultSidebarOpen={false}, collapsing the
//      whole sidebar (References, Properties, Subscribers, Sub-issues, Rounds)
//      behind a panel icon in the top-right corner.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const flags = vi.hoisted(() => ({} as Record<string, boolean>));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: string) => flags[key] ?? false,
}));

vi.mock("@multica/cerebro-rounds", () => ({
  IssueRoundsSection: ({ issueId }: { issueId: string }) => (
    <div data-testid="rounds" data-issue={issueId} />
  ),
}));

vi.mock("@multica/cerebro-references/views", () => ({
  IssueReferenceList: ({ issueId }: { issueId: string }) => (
    <div data-testid="references" data-issue={issueId} />
  ),
}));

import { InboxIssueDetailExtensions } from "./issue-detail-extensions";

afterEach(() => {
  cleanup();
  for (const key of Object.keys(flags)) delete flags[key];
});

describe("InboxIssueDetailExtensions", () => {
  it("mounts References alongside Rounds, matching the issue page's slot", () => {
    flags.cerebro_inbox_rounds = true;
    flags.cerebro_references = true;

    render(<InboxIssueDetailExtensions issueId="issue-1" />);

    expect(screen.getByTestId("references").dataset.issue).toBe("issue-1");
    expect(screen.getByTestId("rounds").dataset.issue).toBe("issue-1");
  });

  it("still mounts References when the Rounds flag is off", () => {
    flags.cerebro_references = true;

    render(<InboxIssueDetailExtensions issueId="issue-1" />);

    expect(screen.getByTestId("references")).not.toBeNull();
    expect(screen.queryByTestId("rounds")).toBeNull();
  });

  it("mounts neither section when both flags are off", () => {
    render(<InboxIssueDetailExtensions issueId="issue-1" />);

    expect(screen.queryByTestId("references")).toBeNull();
    expect(screen.queryByTestId("rounds")).toBeNull();
  });
});

// The sidebar default lives at the call site, not in a component we can render
// here: this surface opted out of IssueDetail's own default by passing
// defaultSidebarOpen={false}. The classic inbox's equivalent assertion lives in
// packages/views/inbox/components/inbox-issue-detail-parity.test.ts.
const dynamicInbox = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), "components/dynamic-inbox.tsx"),
  "utf8",
);

describe("dynamic inbox IssueDetail", () => {
  it("does not collapse the sidebar", () => {
    expect(dynamicInbox).toContain("<IssueDetail");
    expect(dynamicInbox).not.toContain("defaultSidebarOpen={false}");
  });

  it("hands IssueDetail the shared extensions slot", () => {
    expect(dynamicInbox).toContain(
      "extensions={<InboxIssueDetailExtensions issueId=",
    );
  });
});
