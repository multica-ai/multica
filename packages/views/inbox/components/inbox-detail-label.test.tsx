import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import en from "../../locales/en/inbox.json";
import { InboxDetailLabel } from "./inbox-detail-label";

vi.mock("../../issues/components", () => ({
  StatusIcon: () => null,
  PriorityIcon: () => null,
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Someone" }),
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  // Leaf render test: stub the catalog the same way the other data hooks are
  // stubbed, so the component can be mounted without a QueryClientProvider.
  useIssueStatuses: () => ({
    statuses: [],
    activeStatuses: [],
    categoryOf: (key: string) => key,
    labelOf: (key: string) => key,
    entryOf: () => undefined,
    inCategory: () => [],
    isLoaded: true,
  }),
}));

// Resolve accessors against the REAL en locale rather than a stub, so this
// test fails if the unconfirmed case is ever re-pointed at a label that
// carries failure wording.
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (accessor: (dict: unknown) => string, params?: Record<string, string>) => {
      const template = accessor(en);
      if (!params) return template;
      return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => params[key] ?? "");
    },
  }),
}));

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: null,
    title: "Quick create needs a check",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-07-27T08:00:00Z",
    details: null,
    ...overrides,
  };
}

describe("InboxDetailLabel quick-create outcomes", () => {
  const detail = "Couldn't confirm whether the issue was created.";

  it("does not frame an unconfirmed outcome as a failure", () => {
    // GH #5885 follow-up: reusing quick_create_failed rendered this row as
    // "Failed: Couldn't confirm...", asserting a failure never observed.
    const { container } = render(
      <InboxDetailLabel item={item({ type: "quick_create_unconfirmed", details: { error: detail } })} />,
    );

    expect(container.textContent).toBe(detail);
    expect(container.textContent).not.toMatch(/failed/i);
  });

  it("still frames a confirmed failure as a failure", () => {
    const { container } = render(
      <InboxDetailLabel
        item={item({
          type: "quick_create_failed",
          details: { error: "an active issue already exists: JKY-30 (blocked)" },
        })}
      />,
    );

    expect(container.textContent).toBe(
      "Failed: an active issue already exists: JKY-30 (blocked)",
    );
  });

  it("falls back to the neutral type label when an unconfirmed row has no detail", () => {
    const { container } = render(
      <InboxDetailLabel item={item({ type: "quick_create_unconfirmed" })} />,
    );

    expect(container.textContent).toBe(en.types.quick_create_unconfirmed);
    expect(container.textContent).not.toMatch(/failed/i);
  });
});
